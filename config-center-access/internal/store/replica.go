// Package store 管理接入侧 MySQL 节点副本和事务同步游标。
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"fmt"
	"time"

	"github.com/hctty27/middle-platform/config-center-access/internal/remote"
)

// State 与节点副本在同一事务内提交，防止部分同步。
type State struct {
	Epoch    string
	Revision int64
}

// NodeView 仅展示接入服务本地副本的非敏感索引字段。
type NodeView struct {
	ID            int64  `json:"id"`
	GameID        *int64 `json:"game_id"`
	RegionID      *int64 `json:"region_id"`
	NodeType      string `json:"node_type"`
	EnvType       *string `json:"env_type"`
	Code          string `json:"code"`
	Name          string `json:"name"`
	Status        int    `json:"status"`
}

// Replica 使用独立数据库，不直接修改权威配置中心。
type Replica struct{ DB *sql.DB }

// New 绑定本地副本数据库，不与配置中心的权威库共享写连接。
func New(db *sql.DB) *Replica { return &Replica{DB: db} }

// VerifySchema 只读检查人工创建的副本库，空库或旧结构均明确拒绝启动。
func (s *Replica) VerifySchema(ctx context.Context) error {
    var nodeCount,stateCount int
    if err:=s.DB.QueryRowContext(ctx,
        "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE() AND table_name='config_node'").Scan(&nodeCount);err!=nil{return err}
    if err:=s.DB.QueryRowContext(ctx,
        "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE() AND table_name='config_sync_state'").Scan(&stateCount);err!=nil{return err}
    if nodeCount!=1||stateCount!=1 {
        return errors.New("replica schema missing; manually execute config-center-access/db/sql/initialize.sql in an empty config_access database")
    }
	rows, err := s.DB.QueryContext(ctx, `SELECT id,game_id,region_id,env_type,node_type,
		code,name,status,config,remark,created_at,updated_at FROM config_node LIMIT 0`)
	if err != nil {
		return fmt.Errorf("obsolete replica schema: use the pre-baseline code to upgrade before startup: %w", err)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	rows, err = s.DB.QueryContext(ctx, "SELECT epoch,revision,updated_at FROM config_sync_state LIMIT 0")
	if err != nil {
		return fmt.Errorf("invalid replica sync state: %w", err)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	var obsoleteColumns int
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.columns
		WHERE table_schema=DATABASE() AND table_name='config_node'
		AND column_name IN ('parent_id','channel_id','server_group_id')`).Scan(&obsoleteColumns); err != nil {
		return err
	}
	if obsoleteColumns != 0 {
		return errors.New("obsolete replica columns; upgrade the database with pre-baseline scripts first")
	}
	var constraints int
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.table_constraints
		WHERE constraint_schema=DATABASE() AND table_name='config_node'
		AND constraint_name IN ('chk_replica_node_env_type','chk_replica_node_ancestry')`).Scan(&constraints); err != nil {
		return err
	}
	if constraints != 2 {
		return errors.New("replica CHECK constraints missing; upgrade to V6 before startup")
	}
	var clause string
	if err := s.DB.QueryRowContext(ctx, `SELECT check_clause FROM information_schema.check_constraints
		WHERE constraint_schema=DATABASE() AND constraint_name='chk_replica_node_env_type'`).Scan(&clause); err != nil {
		return err
	}
	if strings.Contains(clause, "production") || !strings.Contains(clause, "prod") {
		return errors.New("obsolete replica environment CHECK constraint; upgrade to V6 before startup")
	}
	var stateRows int
	if err := s.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM config_sync_state WHERE id=1").Scan(&stateRows); err != nil {
		return err
	}
	if stateRows != 1 {
		return errors.New("replica sync cursor missing; restore it or rebuild a new empty replica")
	}
	return nil
}

// ReadState 在访问远程服务前读取已提交游标，不持有数据库事务。
func (s *Replica) ReadState(ctx context.Context) (State, error) {
	return readState(ctx, s.DB.QueryRowContext(ctx,
		"SELECT epoch,revision FROM config_sync_state WHERE id=1"))
}

// scanner 抽象 SQL 行扫描接口，供普通查询和事务内查询复用。
type scanner interface{ Scan(...any) error }

func readState(_ context.Context, row scanner) (State, error) {
	var epoch sql.NullString
	var revision int64
	if err := row.Scan(&epoch, &revision); err != nil {
		return State{}, err
	}
	return State{Epoch: epoch.String, Revision: revision}, nil
}

// Apply 对比期望游标，事务性执行全量快照或增量变更。
func (s *Replica) Apply(ctx context.Context, expected, next State, nodes []remote.Node, deleted []int64, full bool) (bool, error) {
	if next.Epoch == "" || next.Revision < 0 {
		return false, fmt.Errorf("invalid target cursor")
	}
	for _, node := range nodes {
		if err := validateNode(node); err != nil {
			return false, err
		}
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	current, err := readState(ctx, tx.QueryRowContext(ctx,
		"SELECT epoch,revision FROM config_sync_state WHERE id=1 FOR UPDATE"))
	if err != nil {
		return false, err
	}
	// CAS 检查游标是否仍为下载数据前的版本，避免多实例同步相互覆盖。
	if current != expected {
		return false, nil
	}
	if full {
		if _, err := tx.ExecContext(ctx, "DELETE FROM config_node"); err != nil {
			return false, err
		}
	}
	for _, id := range deleted {
		if _, err := tx.ExecContext(ctx, "DELETE FROM config_node WHERE id=?", id); err != nil {
			return false, err
		}
	}
	for _, node := range nodes {
		created, err := parseDate(node.CreatedAt)
		if err != nil { return false, err }
		updated, err := parseDate(node.UpdatedAt)
		if err != nil { return false, err }
		_, err = tx.ExecContext(ctx, `INSERT INTO config_node
			(id,game_id,region_id,node_type,env_type,code,name,status,config,remark,created_at,updated_at)
            VALUES(?,?,?,?,?,?,?,?,?,?,?,?)
			ON DUPLICATE KEY UPDATE
			game_id=VALUES(game_id),region_id=VALUES(region_id),
			node_type=VALUES(node_type),env_type=VALUES(env_type),
			code=VALUES(code),name=VALUES(name),status=VALUES(status),
			config=VALUES(config),remark=VALUES(remark),
			created_at=VALUES(created_at),updated_at=VALUES(updated_at)`,
			node.ID,node.GameID,node.RegionID,node.NodeType,node.EnvType,node.Code,
			node.Name,node.Status,node.Config,node.Remark,created,updated)
		if err != nil { return false, fmt.Errorf("store node %d: %w", node.ID, err) }
	}
	var epoch any = next.Epoch
	if next.Epoch == "" { epoch = nil }
	res, err := tx.ExecContext(ctx, `UPDATE config_sync_state
		SET epoch=?,revision=?,updated_at=UTC_TIMESTAMP(6) WHERE id=1`,
		epoch, next.Revision)
	if err != nil { return false, err }
	count, err := res.RowsAffected()
	if err != nil || count != 1 { return false, fmt.Errorf("update cursor rows=%d: %w", count, err) }
	if err := tx.Commit(); err != nil { return false, err }
	return true, nil
}

// validateNode 在持久化前校验远端节点的必填项、环境枚举和日期字段。
func validateNode(n remote.Node) error {
	if n.ID <= 0 || n.Code == "" || n.Name == "" || !json.Valid([]byte(n.Config)) {
		return fmt.Errorf("invalid node id=%d or JSON config", n.ID)
	}
	if n.NodeType == "region" {
        if n.EnvType != nil {
            switch *n.EnvType {
            case "test", "review", "prod":
            default: return fmt.Errorf("invalid region environment %q", *n.EnvType)
            }
        }
    } else if n.EnvType != nil {
        return fmt.Errorf("non-region node with environment")
    }
    if _, err := parseDate(n.CreatedAt); err != nil { return err }
	if _, err := parseDate(n.UpdatedAt); err != nil { return err }
	return nil
}

func parseDate(v string) (time.Time, error) {
	parsed, err := time.Parse("2006-01-02T15:04:05.999999999", v)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid config node datetime %q: %w", v, err)
	}
	return parsed, nil
}

// Nodes 查询本地配置副本，不触发远程网络访问。
func (s *Replica) Nodes(ctx context.Context) ([]NodeView, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id,game_id,region_id,node_type,env_type,code,name,status
        FROM config_node ORDER BY id`)
	if err != nil { return nil, err }
	defer rows.Close()
	out := make([]NodeView, 0)
	for rows.Next() {
		var n NodeView
		var game, region sql.NullInt64
        var envType sql.NullString
		if err := rows.Scan(&n.ID, &game, &region, &n.NodeType, &envType, &n.Code, &n.Name, &n.Status); err != nil { return nil, err }
		if game.Valid { n.GameID = &game.Int64 }
		if region.Valid { n.RegionID = &region.Int64 }
        if envType.Valid { n.EnvType = &envType.String }
		out = append(out, n)
	}
	return out, rows.Err()
}

// Revision 返回本地游标与最后一次事务提交的时间。
func (s *Replica) Revision(ctx context.Context) (map[string]any, error) {
	var epoch sql.NullString
	var rev int64
	var updated time.Time
	err := s.DB.QueryRowContext(ctx,
		"SELECT epoch,revision,updated_at FROM config_sync_state WHERE id=1").Scan(&epoch, &rev, &updated)
	if err != nil { return nil, err }
	var val any
	if epoch.Valid { val = epoch.String }
	return map[string]any{"epoch": val, "revision": rev,
		"updated_at": updated.UTC().Format("2006-01-02T15:04:05.999999")}, nil
}

// Ready 用真实数据库 Ping 校验服务是否可工作。
func (s *Replica) Ready(ctx context.Context) error {
	if s.DB == nil { return errors.New("missing replica database") }
	return s.DB.PingContext(ctx)
}
