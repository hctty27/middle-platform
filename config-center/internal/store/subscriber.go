package store

import (
	"context"
	"database/sql"
	"strings"
	"github.com/hctty27/middle-platform/config-center/internal/webhook"
)

// Subscribers 获取所有回调订阅者及对应投递进度。
func (s *Store) Subscribers(ctx context.Context) ([]Subscriber, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT s.id,s.code,s.callback_url,s.enabled,d.acknowledged_revision,d.failure_count,d.last_error FROM config_subscriber s JOIN config_delivery_state d ON d.subscriber_id=s.id ORDER BY s.id")
	if err != nil { return nil, err }
	defer rows.Close()
	out := make([]Subscriber, 0)
	for rows.Next() {
		var sub Subscriber
		var last sql.NullString
		if err := rows.Scan(&sub.ID, &sub.Code, &sub.CallbackURL, &sub.Enabled, &sub.AcknowledgedRevision, &sub.Failures, &last); err != nil { return nil, err }
		if last.Valid { sub.LastError = &last.String }
		out = append(out, sub)
	}
	return out, rows.Err()
}

// CreateSubscriber 原子创建订阅者与初始投递状态。
func (s *Store) CreateSubscriber(ctx context.Context, input CreateSubscriberRequest) error {
	if strings.TrimSpace(input.Code) == "" || len([]rune(input.Code)) > 128 ||
		strings.TrimSpace(input.Secret) == "" || len(input.Secret) > 255 ||
		webhook.ValidateCallback(input.CallbackURL, s.webhookAllowedHosts()) != nil {
		return ErrInvalid
	}
	// 订阅者及投递游标必须同时写入，防止回调任务出现无状态的订阅者。
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil { return err }
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `INSERT INTO config_subscriber(code,callback_url,webhook_secret,enabled,created_at,updated_at)
		VALUES(?,?,?,1,UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))`, input.Code, input.CallbackURL, input.Secret)
	if err != nil { return databaseError(err) }
	id, err := result.LastInsertId()
	if err != nil { return err }
	_, err = tx.ExecContext(ctx, `INSERT INTO config_delivery_state(subscriber_id,next_attempt_at,updated_at)
		VALUES(?,UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))`, id)
	if err != nil { return err }
	return tx.Commit()
}

// webhookAllowedHosts 返回注册回调时采用的目标主机白名单。
func (s *Store) webhookAllowedHosts() string {
	if s.AllowedHosts != "" { return s.AllowedHosts }
	return "localhost,127.0.0.1"
}
