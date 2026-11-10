package store

import (
	"context"
	"database/sql"
	"errors"
	"hitall.com/middle-platform/config-center/internal/webhook"
	"strings"
)

// Subscribers 获取所有回调订阅者及对应投递进度。
func (s *Store) Subscribers(ctx context.Context) ([]Subscriber, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT s.id,s.code,s.callback_url,s.enabled,d.acknowledged_revision,d.failure_count,d.last_error FROM config_subscriber s JOIN config_subscriber_delivery_state d ON d.subscriber_id=s.id ORDER BY s.id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Subscriber, 0)
	for rows.Next() {
		var sub Subscriber
		var last sql.NullString
		if err := rows.Scan(&sub.ID, &sub.Code, &sub.CallbackURL, &sub.Enabled, &sub.AcknowledgedRevision, &sub.Failures, &last); err != nil {
			return nil, err
		}
		if last.Valid {
			sub.LastError = &last.String
		}
		out = append(out, sub)
	}
	return out, rows.Err()
}

// CreateSubscriber 原子创建订阅者与初始投递状态。
func (s *Store) CreateSubscriber(ctx context.Context, input CreateSubscriberRequest) error {
	if strings.TrimSpace(input.Code) == "" || len([]rune(input.Code)) > 128 ||
		len([]byte(input.Secret)) < 32 || len(input.Secret) > 255 || strings.TrimSpace(input.Secret) != input.Secret ||
		webhook.ValidateCallback(input.CallbackURL) != nil {
		return ErrInvalid
	}
	// 订阅者及投递游标必须同时写入，防止回调任务出现无状态的订阅者。
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `INSERT INTO config_subscriber(code,callback_url,signing_secret,enabled,created_at,updated_at)
		VALUES(?,?,?,1,UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))`, input.Code, input.CallbackURL, input.Secret)
	if err != nil {
		return databaseError(err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO config_subscriber_delivery_state(subscriber_id,next_attempt_at,updated_at)
		VALUES(?,UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))`, id)
	if err != nil {
		return err
	}
	return tx.Commit()
}


// lockSubscriber 在事务内锁定订阅者，并区分不存在与数据库故障。
func lockSubscriber(ctx context.Context, tx *sql.Tx, id int64) error {
	if id <= 0 {
		return ErrInvalid
	}
	var found int64
	err := tx.QueryRowContext(ctx, "SELECT id FROM config_subscriber WHERE id=? FOR UPDATE", id).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrSubscriberNotFound
	}
	return err
}

// UpdateSubscriber 修改回调地址；密钥为空时保留现有值。
func (s *Store) UpdateSubscriber(ctx context.Context, id int64, input UpdateSubscriberRequest) error {
	if id <= 0 || webhook.ValidateCallback(input.CallbackURL) != nil || (input.Secret != "" && (len([]byte(input.Secret)) < 32 || len(input.Secret) > 255 || strings.TrimSpace(input.Secret) != input.Secret)) {
		return ErrInvalid
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := lockSubscriber(ctx, tx, id); err != nil {
		return err
	}
	if input.Secret == "" {
		_, err = tx.ExecContext(ctx, `UPDATE config_subscriber
			SET callback_url=?,updated_at=UTC_TIMESTAMP(6) WHERE id=?`, input.CallbackURL, id)
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE config_subscriber
			SET callback_url=?,webhook_secret=?,updated_at=UTC_TIMESTAMP(6) WHERE id=?`,
			input.CallbackURL, input.Secret, id)
	}
	if err != nil {
		return databaseError(err)
	}
	return tx.Commit()
}

// SetSubscriberEnabled 独立启用或停用订阅者，不重置投递游标。
func (s *Store) SetSubscriberEnabled(ctx context.Context, id int64, enabled bool) error {
	if id <= 0 {
		return ErrInvalid
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := lockSubscriber(ctx, tx, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE config_subscriber
		SET enabled=?,updated_at=UTC_TIMESTAMP(6) WHERE id=?`, enabled, id); err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteSubscriber 删除订阅者；外键级联清理其投递状态。
func (s *Store) DeleteSubscriber(ctx context.Context, id int64) error {
	if id <= 0 {
		return ErrInvalid
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := lockSubscriber(ctx, tx, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM config_subscriber WHERE id=?", id); err != nil {
		return databaseError(err)
	}
	return tx.Commit()
}


// SubscriberSecret 按接入端编码读取启用订阅者的独立签名密钥。
func (s *Store) SubscriberSecret(ctx context.Context, code string) (string, bool, error) {
	if code == "" || len([]rune(code)) > 128 || strings.TrimSpace(code) != code || strings.ContainsAny(code, "\\r\\n") {
		return "", false, nil
	}
	var secret string
	var enabled bool
	err := s.DB.QueryRowContext(ctx, `SELECT signing_secret,enabled FROM config_subscriber WHERE code=?`, code).Scan(&secret, &enabled)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return secret, enabled, nil
}
