package webhook

import (
	"bytes"
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
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// Delivery 描述一次待发送的签名回调及其数据库租约。
// Delivery 保存一次待投递的订阅通知及其版本和租约信息。
type Delivery struct {
	SubscriberID int64
	URL          string
	Secret       string
	Epoch        string
	Revision     int64
	LeaseID      string
}

// Dispatcher 控制回调任务领取、并发发送与重试。
type Dispatcher struct {
	DB           *sql.DB
	Client       *http.Client
	BatchSize    int
	Concurrency  int
}

// NewDispatcher 创建不跟随重定向并校验连接目标的回调客户端。
func NewDispatcher(db *sql.DB) *Dispatcher {
	transport := &http.Transport{
		DialContext: secureWebhookDial(nil),
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
		BatchSize: 16,
		Concurrency: 4,
	}
}

// claim 通过数据库行锁与租约协调集群任务领取。
func (d *Dispatcher) claim(ctx context.Context) (*Delivery, error) {
	tx, err := d.DB.BeginTx(ctx, nil)
	if err != nil { return nil, err }
	defer tx.Rollback()
	delivery := &Delivery{}
	// SKIP LOCKED 让多实例跳过已被领取的任务，避免相互阻塞与重复投递。
	err = tx.QueryRowContext(ctx, `SELECT d.subscriber_id,s.callback_url,s.webhook_secret,r.epoch,r.revision
		FROM config_subscriber_delivery_state d
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
	// 随机租约标识用于回写时的所有权校验，过期任务不能覆盖新领取者的结果。
	delivery.LeaseID = hex.EncodeToString(id[:])
	result, err := tx.ExecContext(ctx, `UPDATE config_subscriber_delivery_state
		SET lease_owner=?,lease_until=DATE_ADD(UTC_TIMESTAMP(6),INTERVAL 30 SECOND)
		WHERE subscriber_id=?`, delivery.LeaseID, delivery.SubscriberID)
	if err != nil { return nil, err }
	rows, err := result.RowsAffected()
	if err != nil || rows != 1 { return nil, fmt.Errorf("领取 Webhook 投递租约失败: %v", err) }
	if err := tx.Commit(); err != nil { return nil, err }
	return delivery, nil
}

// send 计算 HMAC 签名并发出回调通知。
// send 校验回调目标并发送签名通知，不把配置内容直接传给接入端。
func (d *Dispatcher) send(ctx context.Context, job Delivery) error {
	if err := ValidateCallback(job.URL); err != nil {
		return fmt.Errorf("Webhook 通知目标不安全: %w", err)
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
		return fmt.Errorf("Webhook 返回 HTTP %d", response.StatusCode)
	}
	return nil
}

// delivered 在租约有效时更新已确认的同步版本。
func (d *Dispatcher) delivered(ctx context.Context, job Delivery) error {
	_, err := d.DB.ExecContext(ctx, `UPDATE config_subscriber_delivery_state
		SET acknowledged_revision=CASE WHEN acknowledged_epoch=?
			THEN GREATEST(acknowledged_revision,?) ELSE ? END,
			acknowledged_epoch=?,
			lease_owner=NULL,lease_until=NULL,failure_count=0,last_error=NULL,
			next_attempt_at=UTC_TIMESTAMP(6),updated_at=UTC_TIMESTAMP(6)
		WHERE subscriber_id=? AND lease_owner=?`,
		job.Epoch, job.Revision, job.Revision, job.Epoch, job.SubscriberID, job.LeaseID)
	return err
}

// failed 记录失败信息并按指数退避安排重试。
func (d *Dispatcher) failed(ctx context.Context, job Delivery, reason error) error {
	message := []rune(reason.Error())
	if len(message) > 500 { message = message[:500] }
	// 指数退避上限为五分钟，防止故障订阅者持续消耗回调资源。
	_, err := d.DB.ExecContext(ctx, `UPDATE config_subscriber_delivery_state
		SET lease_owner=NULL,lease_until=NULL,failure_count=failure_count+1,
			next_attempt_at=DATE_ADD(UTC_TIMESTAMP(6),
				INTERVAL LEAST(300,POW(2,LEAST(failure_count+1,8))) SECOND),
			last_error=?,updated_at=UTC_TIMESTAMP(6)
		WHERE subscriber_id=? AND lease_owner=?`,
		string(message), job.SubscriberID, job.LeaseID)
	return err
}

// dispatch 控制单轮任务数量及并发上限。
// dispatch 并发领取和投递一批到期任务，避免单个回调阻塞其他订阅者。
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
			// 优雅停机时使用独立的有界上下文，确保仍能持久化投递结果。
			outcomeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err == nil {
				err = d.delivered(outcomeCtx, job)
			} else {
				if dbErr := d.failed(outcomeCtx, job, err); dbErr != nil {
					slog.Error("更新 Webhook 失败状态失败", "subscriber", job.SubscriberID, "error", dbErr)
				}
			}
			if err != nil { slog.Warn("Webhook 投递失败", "subscriber", job.SubscriberID, "error", err) }
		}(job)
	}
	wg.Wait()
	return ctx.Err()
}

// Run 定时领取并发送回调，响应优雅停机。
// Run 循环处理待投递任务，并在上下文取消后停止。
func (d *Dispatcher) Run(ctx context.Context) {
	if d.Client == nil { d.Client = NewDispatcher(d.DB).Client }
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		if err := d.dispatch(ctx); err != nil && !errors.Is(err, context.Canceled) {
			slog.Error("Webhook 分发失败", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

 // Send 对指定任务发送签名回调，集成测试通过此入口验证协议。
func (d *Dispatcher) Send(ctx context.Context, job Delivery) error { return d.send(ctx, job) }
// Claim 领取一个任务，集成测试通过此入口验证数据库租约。
func (d *Dispatcher) Claim(ctx context.Context) (*Delivery, error) { return d.claim(ctx) }
// Delivered 提交已确认版本，集成测试通过此入口验证确认逻辑。
func (d *Dispatcher) Delivered(ctx context.Context, job Delivery) error { return d.delivered(ctx, job) }
