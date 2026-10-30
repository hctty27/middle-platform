package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/go-sql-driver/mysql"
	"github.com/hctty27/middle-platform/config-center/internal/service"
	"github.com/hctty27/middle-platform/config-center/internal/sqlcgen"
)

// databaseError 将已知的 MySQL 错误转换为业务冲突。
func databaseError(err error) error {
	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) {
		if mysqlErr.Number == 1062 { return fmt.Errorf("%w: duplicate sibling code", ErrConflict) }
		if mysqlErr.Number == 3819 || mysqlErr.Number == 1451 { return fmt.Errorf("%w: database constraint", ErrConflict) }
	}
	return err
}

// recordChange 在同一事务内递增版本并记录节点事件。
func recordChange(ctx context.Context, tx *sql.Tx, nodeID int64, operation string) error {
	queries := sqlcgen.New(tx)
	if err := queries.IncrementRevision(ctx); err != nil { return err }
	current, err := queries.GetRevision(ctx)
	if err != nil { return err }
	_, err = tx.ExecContext(ctx, "INSERT INTO config_change_log(revision,node_id,operation,created_at) VALUES(?,?,?,UTC_TIMESTAMP(6))", current.Revision, nodeID, operation)
	return err
}

// lockRevision 锁定全局版本行，避免并发写入版本乱序。
func lockRevision(ctx context.Context, tx *sql.Tx) error {
	_, err := sqlcgen.New(tx).LockRevision(ctx)
	return err
}

// Create 校验父子关系后原子写入节点及版本事件。
func (s *Store) Create(ctx context.Context, input CreateNodeRequest) (Node, error) {
	if err := service.ValidateCreate(input); err != nil { return Node{}, err }
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil { return Node{}, err }
	defer tx.Rollback()
	// 在节点变更前锁定全局版本行，保证多实例写入的版本顺序。
	if err := lockRevision(ctx, tx); err != nil { return Node{}, err }

	var gameID, regionID, groupID *int64
	if input.NodeType != "game" {
		parent, err := scanNode(tx.QueryRowContext(ctx, "SELECT "+nodeColumns+" FROM config_node WHERE id=?", input.ParentID))
		if errors.Is(err, sql.ErrNoRows) { return Node{}, ErrInvalid }
		if err != nil { return Node{}, err }
		switch input.NodeType {
		case "region":
			if parent.NodeType != "game" { return Node{}, ErrInvalid }
			gameID = &parent.ID
		case "server_group":
			if parent.NodeType != "region" { return Node{}, ErrInvalid }
			gameID, regionID = parent.GameID, &parent.ID
		case "channel_app":
			if parent.NodeType != "server_group" { return Node{}, ErrInvalid }
			gameID, regionID, groupID = parent.GameID, parent.RegionID, &parent.ID
		}
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO config_node(game_id,region_id,server_group_id,node_type,env_type,code,name,status,config,remark,created_at,updated_at)
        VALUES(?,?,?,?,?,?,?,?,?,?,UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))`,
		gameID, regionID, groupID, input.NodeType, input.EnvType, input.Code, input.Name, *input.Status, input.Config, input.Remark)
	if err != nil { return Node{}, databaseError(err) }
	id, err := result.LastInsertId()
	if err != nil { return Node{}, err }
	if err := recordChange(ctx, tx, id, "UPSERT"); err != nil { return Node{}, err }
	node, err := scanNode(tx.QueryRowContext(ctx, "SELECT "+nodeColumns+" FROM config_node WHERE id=?", id))
	if err != nil { return Node{}, err }
	if err := tx.Commit(); err != nil { return Node{}, err }
	return node, nil
}

// Update 仅在字段确实变化时更新数据库和全局版本。
func (s *Store) Update(ctx context.Context, id int64, input UpdateNodeRequest) (Node, error) {
	if id <= 0 { return Node{}, ErrInvalid }
	if err := service.ValidateNode(input.Name, input.Status, input.Config, input.Remark); err != nil { return Node{}, err }
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil { return Node{}, err }
	defer tx.Rollback()
	if err := lockRevision(ctx, tx); err != nil { return Node{}, err }
	current, err := scanNode(tx.QueryRowContext(ctx, "SELECT "+nodeColumns+" FROM config_node WHERE id=?", id))
    if err != nil { return Node{}, err }
    if err := service.ValidateEnvType(current.NodeType, input.EnvType); err != nil { return Node{}, err }
    if current.NodeType == "region" && current.EnvType != nil && *current.EnvType != *input.EnvType {
        return Node{}, ErrConflict
    }
    var unchanged sql.NullBool
	if err := tx.QueryRowContext(ctx, `SELECT (name <=> ?) AND (status <=> ?) AND (remark <=> ?)
		AND (config = CAST(? AS JSON)) AND (env_type <=> ?) FROM config_node WHERE id=?`,
		input.Name, *input.Status, input.Remark, input.Config, input.EnvType, id).Scan(&unchanged); err != nil {
		return Node{}, err
	}
	if !unchanged.Valid || !unchanged.Bool {
		_, err := tx.ExecContext(ctx, `UPDATE config_node SET name=?,status=?,config=?,remark=?,env_type=?,updated_at=UTC_TIMESTAMP(6) WHERE id=?`,
			input.Name, *input.Status, input.Config, input.Remark, input.EnvType, id)
		if err != nil { return Node{}, databaseError(err) }
		if err := recordChange(ctx, tx, id, "UPSERT"); err != nil { return Node{}, err }
		current, err = scanNode(tx.QueryRowContext(ctx, "SELECT "+nodeColumns+" FROM config_node WHERE id=?", id))
		if err != nil { return Node{}, err }
	}
	if err := tx.Commit(); err != nil { return Node{}, err }
	return current, nil
}

// Delete 拒绝删除仍有子节点的配置并记录删除事件。
func (s *Store) Delete(ctx context.Context, id int64) error {
	if id <= 0 { return ErrInvalid }
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil { return err }
	defer tx.Rollback()
	if err := lockRevision(ctx, tx); err != nil { return err }
	current, err := scanNode(tx.QueryRowContext(ctx, "SELECT "+nodeColumns+" FROM config_node WHERE id=?", id))
	if err != nil { return err }
	var childCondition string
	switch current.NodeType {
	case "game":
		childCondition = "node_type='region' AND game_id=?"
	case "region":
		childCondition = "node_type='server_group' AND region_id=?"
	case "server_group":
		childCondition = "node_type='channel_app' AND server_group_id=?"
	case "channel_app":
		childCondition = "1=0"
	default:
		return ErrInvalid
	}
	var count int64
	if current.NodeType != "channel_app" {
		if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM config_node WHERE "+childCondition, id).Scan(&count); err != nil {
			return err
		}
		if count > 0 { return ErrConflict }
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM config_node WHERE id=?", id); err != nil { return databaseError(err) }
	if err := recordChange(ctx, tx, id, "DELETE"); err != nil { return err }
	return tx.Commit()
}
