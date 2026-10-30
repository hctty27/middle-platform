package store

import (
	"context"
	"database/sql"
	"github.com/hctty27/middle-platform/config-center/internal/sqlcgen"
)

// Revision 读取全局同步版本号。
func (s *Store) Revision(ctx context.Context) (Revision, error) {
	return revision(ctx, s.DB)
}

// revision 在数据库连接及事务间复用版本读取。
func revision(ctx context.Context, db sqlcgen.DBTX) (Revision, error) {
	row, err := sqlcgen.New(db).GetRevision(ctx)
	if err != nil { return Revision{}, err }
	return Revision{Epoch: row.Epoch, Revision: row.Revision}, nil
}

// Changes 分页读取全局版本之后的变更记录。
func (s *Store) Changes(ctx context.Context, after int64, limit int) ([]ChangeEvent, error) {
	if after < 0 || limit < 1 || limit > 500 { return nil, ErrInvalid }
	found, err := sqlcgen.New(s.DB).ListChanges(ctx, sqlcgen.ListChangesParams{
		Revision: after, Limit: int32(limit),
	})
	if err != nil { return nil, err }
	out := make([]ChangeEvent, 0, len(found))
	for _, event := range found {
		out = append(out, ChangeEvent{
			Revision: event.Revision,
			NodeID: event.NodeID,
			Operation: event.Operation,
			CreatedAt: LocalDateTime(event.CreatedAt),
		})
	}
	return out, nil
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
