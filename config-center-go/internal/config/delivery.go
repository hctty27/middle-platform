package config

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"bytes"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

type Delivery struct {
	SubscriberID int64
	URL          string
	Secret       string
	Epoch        string
	Revision     int64
	LeaseID      string
}

type Dispatcher struct {
	DB           *sql.DB
	Client       *http.Client
	AllowedHosts string
	BatchSize    int
	Concurrency  int
}

func NewDispatcher(db *sql.DB, allowedHosts string) *Dispatcher {
	if allowedHosts == "" { allowedHosts = "localhost,127.0.0.1" }
	transport := &http.Transport{
		DialContext: secureWebhookDial(allowedHosts, nil),
		MaxIdleConns: 20,
		MaxIdleConnsPerHost: 4,
		IdleConnTimeout: 30 * time.Second,
	}
	return &Dispatcher{
		DB: db,
		Client: &http.Client{
			Timeout: 5 * time.Second,
			Transport: transport,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
		AllowedHosts: allowedHosts,
		BatchSize: 16,
		Concurrency: 4,
	}
}

func (d *Dispatcher) claim(ctx context.Context) (*Delivery, error) {
	tx, err := d.DB.BeginTx(ctx, nil)
	if err != nil { return nil, err }
	defer tx.Rollback()
	delivery := &Delivery{}
	err = tx.QueryRowContext(ctx, `SELECT d.subscriber_id,s.callback_url,s.webhook_secret,r.epoch,r.revision
		FROM config_delivery_state d
		JOIN config_subscriber s ON s.id=d.subscriber_id
		JOIN config_revision r ON r.id=1
		WHERE s.enabled=1 AND
			(d.acknowledged_epoch IS NULL OR d.acknowledged_epoch<>r.epoch OR d.acknowledged_revision<r.revision)
			AND d.next_attempt_at<=UTC_TIMESTAMP(6)
			AND (d.lease_until IS NULL OR d.lease_until<UTC_TIMESTAMP(6))
		ORDER BY d.next_attempt_at,d.subscriber_id
		LIMIT 1 FOR UPDATE OF d SKIP LOCKED`).Scan(
		&delivery.SubscriberID, &delivery.URL, &delivery.Secret, &delivery.Epoch, &delivery.Revision)
	if errors.Is(err, sql.ErrNoRows) { return nil, nil }
	if err != nil { return nil, err }
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil { return nil, err }
	delivery.LeaseID = hex.EncodeToString(id[:])
	result, err := tx.ExecContext(ctx, `UPDATE config_delivery_state
		SET lease_owner=?,lease_until=DATE_ADD(UTC_TIMESTAMP(6),INTERVAL 30 SECOND)
		WHERE subscriber_id=?`, delivery.LeaseID, delivery.SubscriberID)
	if err != nil { return nil, err }
	rows, err := result.RowsAffected()
	if err != nil || rows != 1 { return nil, fmt.Errorf("failed to claim webhook lease: %v", err) }
	if err := tx.Commit(); err != nil { return nil, err }
	return delivery, nil
}

func (d *Dispatcher) send(ctx context.Context, job Delivery) error {
	if err := validateCallback(job.URL, d.AllowedHosts); err != nil {
		return fmt.Errorf("unsafe webhook destination: %w", err)
	}
	timestamp := time.Now().Unix()
	message := fmt.Sprintf("%d.%s.%d", timestamp, job.Epoch, job.Revision)
	sign := hmac.New(sha256.New, []byte(job.Secret))
	_, _ = io.WriteString(sign, message)
	body, err := json.Marshal(struct {
		Epoch string `json:"epoch"`
		Revision int64 `json:"revision"`
	}{job.Epoch, job.Revision})
	if err != nil { return err }
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, job.URL, bytes.NewReader(body))
	if err != nil { return err }
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Config-Timestamp", fmt.Sprint(timestamp))
	req.Header.Set("X-Config-Signature", "sha256="+hex.EncodeToString(sign.Sum(nil)))
	response, err := d.Client.Do(req)
	if err != nil { return err }
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("webhook HTTP %d", response.StatusCode)
	}
	return nil
}

func (d *Dispatcher) delivered(ctx context.Context, job Delivery) error {
	_, err := d.DB.ExecContext(ctx, `UPDATE config_delivery_state
		SET acknowledged_revision=CASE WHEN acknowledged_epoch=?
			THEN GREATEST(acknowledged_revision,?) ELSE ? END,
			acknowledged_epoch=?,
			lease_owner=NULL,lease_until=NULL,failure_count=0,last_error=NULL,
			next_attempt_at=UTC_TIMESTAMP(6),updated_at=UTC_TIMESTAMP(6)
		WHERE subscriber_id=? AND lease_owner=?`,
		job.Epoch, job.Revision, job.Revision, job.Epoch, job.SubscriberID, job.LeaseID)
	return err
}

func (d *Dispatcher) failed(ctx context.Context, job Delivery, reason error) error {
	message := []rune(reason.Error())
	if len(message) > 500 { message = message[:500] }
	_, err := d.DB.ExecContext(ctx, `UPDATE config_delivery_state
		SET lease_owner=NULL,lease_until=NULL,failure_count=failure_count+1,
			next_attempt_at=DATE_ADD(UTC_TIMESTAMP(6),
				INTERVAL LEAST(300,POW(2,LEAST(failure_count+1,8))) SECOND),
			last_error=?,updated_at=UTC_TIMESTAMP(6)
		WHERE subscriber_id=? AND lease_owner=?`,
		string(message), job.SubscriberID, job.LeaseID)
	return err
}

func (d *Dispatcher) dispatch(ctx context.Context) error {
	batchSize := d.BatchSize
	if batchSize <= 0 || batchSize > 64 { batchSize = 16 }
	concurrency := d.Concurrency
	if concurrency <= 0 || concurrency > 16 { concurrency = 4 }
	jobs := make([]Delivery, 0, batchSize)
	for len(jobs) < batchSize {
		job, err := d.claim(ctx)
		if err != nil { return err }
		if job == nil { break }
		jobs = append(jobs, *job)
	}
	if len(jobs) == 0 { return nil }
	limit := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	for _, job := range jobs {
		if ctx.Err() != nil { break }
		limit <- struct{}{}
		wg.Add(1)
		go func(job Delivery) {
			defer wg.Done()
			defer func() { <-limit }()
			err := d.send(ctx, job)
			// Use an independent bounded context to record outcomes during graceful shutdown.
			outcomeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err == nil {
				err = d.delivered(outcomeCtx, job)
			} else {
				if dbErr := d.failed(outcomeCtx, job, err); dbErr != nil {
					slog.Error("webhook failure state update failed", "subscriber", job.SubscriberID, "error", dbErr)
				}
			}
			if err != nil { slog.Warn("webhook delivery failed", "subscriber", job.SubscriberID, "error", err) }
		}(job)
	}
	wg.Wait()
	return ctx.Err()
}

func (d *Dispatcher) Run(ctx context.Context) {
	if d.Client == nil { d.Client = NewDispatcher(d.DB, d.AllowedHosts).Client }
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		if err := d.dispatch(ctx); err != nil && !errors.Is(err, context.Canceled) {
			slog.Error("webhook dispatch failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
