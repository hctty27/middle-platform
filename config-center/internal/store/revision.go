package store

import (
	"context"
	"database/sql"
	"time"
)

// Revision 读取全局同步版本号。
func (s *Store) Revision(ctx context.Context) (Revision, error) {
	return revision(ctx, s.DB)
}

// revision 在数据库连接及事务间复用版本读取。
func revision(ctx context.Context, db queryer) (Revision, error) {
	var epoch string
	var rev int64
	err := db.QueryRowContext(ctx, "SELECT epoch, revision FROM config_revision WHERE id=1").Scan(&epoch, &rev)
	if err != nil {
		return Revision{}, err
	}
	return Revision{Epoch: epoch, Revision: rev}, nil
}

// Changes 分页读取全局版本之后的变更记录。
func (s *Store) Changes(ctx context.Context, after int64, limit int) ([]ChangeEvent, error) {
	if after < 0 || limit < 1 || limit > 500 {
		return nil, ErrInvalid
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT revision, node_id, operation, created_at
		FROM config_node_change_log WHERE revision > ? ORDER BY revision LIMIT ?`, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ChangeEvent, 0)
	for rows.Next() {
		var event ChangeEvent
		var created time.Time
		if err := rows.Scan(&event.Revision, &event.NodeID, &event.Operation, &created); err != nil {
			return nil, err
		}
		event.CreatedAt = LocalDateTime(created)
		out = append(out, event)
	}
	return out, rows.Err()
}

// Snapshot 在可重复读事务内读取一致性配置快照。
func (s *Store) Snapshot(ctx context.Context) (Snapshot, error) {
	// 快照中的 epoch、revision 和节点列表必须来自同一事务视图。
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil { return Snapshot{}, err }
	defer tx.Rollback()
	v, err := revision(ctx, tx)
	if err != nil { return Snapshot{}, err }
	found, err := nodes(ctx, tx, "SELECT "+nodeColumns+" FROM config_node ORDER BY id")
	if err != nil { return Snapshot{}, err }
	if err := tx.Commit(); err != nil { return Snapshot{}, err }
	return Snapshot{Epoch: v.Epoch, Revision: v.Revision, Nodes: found}, nil
}
