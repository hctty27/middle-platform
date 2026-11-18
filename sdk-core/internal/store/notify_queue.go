package store

import (
	"context"
	"errors"

	"hitall.com/middle-platform/sdk-core/internal/model"
)

// ListDue 只读预览到期任务，分页游标不代表租约；领取时必须重新校验状态。
func (s *OrderStore) ListDue(ctx context.Context, after *model.NotifyCandidate, limit int) ([]model.NotifyCandidate, error) {
	if limit < 1 || limit > 128 {
		return nil, errors.New("通知候选批量必须在 1 到 128 之间")
	}
	query := `SELECT id,game_notify_url,next_notify_at,UTC_TIMESTAMP() FROM game_order
		WHERE payment_status=1 AND notify_status<>1 AND next_notify_at<=UTC_TIMESTAMP()`
	args := []any{}
	if after != nil {
		query += ` AND next_notify_at<=? AND (next_notify_at>? OR (next_notify_at=? AND id>?))`
		args = append(args, after.DueBefore, after.NextNotifyAt, after.NextNotifyAt, after.ID)
	}
	query += ` ORDER BY next_notify_at,id LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	candidates := make([]model.NotifyCandidate, 0, limit)
	for rows.Next() {
		var item model.NotifyCandidate
		if err := rows.Scan(&item.ID, &item.GameNotifyURL, &item.NextNotifyAt, &item.DueBefore); err != nil {
			return nil, err
		}
		if after != nil {
			item.DueBefore = after.DueBefore
		}
		candidates = append(candidates, item)
	}
	return candidates, rows.Err()
}

// ClaimCandidate 在取得本地目标额度后领取；其他实例已领取、完成或更改的候选直接跳过。
func (s *OrderStore) ClaimCandidate(ctx context.Context, candidate model.NotifyCandidate) (*model.Order, error) {
	return retryClaim(ctx, func() (*model.Order, error) {
		return s.claimOne(ctx, `SELECT `+claimColumns+` FROM game_order
			WHERE id=? AND payment_status=1 AND notify_status<>1
			  AND next_notify_at<=UTC_TIMESTAMP()
			  AND BINARY game_notify_url=BINARY ?
			FOR UPDATE SKIP LOCKED`, candidate.ID, candidate.GameNotifyURL)
	})
}
