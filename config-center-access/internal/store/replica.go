// Package store 管理接入侧 MySQL 节点副本和事务同步游标。
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
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

// EnsureSchema 对全新数据库创建三级配置树副本；旧库需先完成手工迁移。
func (s *Replica) EnsureSchema(ctx context.Context) error {
	const nodes = `CREATE TABLE IF NOT EXISTS config_node (
		id BIGINT NOT NULL PRIMARY KEY,
		game_id BIGINT NULL,
		region_id BIGINT NULL,
		node_type VARCHAR(20) NOT NULL,
		env_type VARCHAR(16) COLLATE utf8mb4_bin NULL,
		code VARCHAR(128) COLLATE utf8mb4_bin NOT NULL,
		name VARCHAR(128) NOT NULL,
		status TINYINT NOT NULL,
		config JSON NOT NULL,
		remark VARCHAR(500) NULL,
		created_at DATETIME(6) NOT NULL,
		updated_at DATETIME(6) NOT NULL,
		KEY idx_replica_game_type (game_id,node_type),
		KEY idx_replica_region_type (region_id,node_type),
		CONSTRAINT chk_replica_node_env_type CHECK (
            (node_type='region' AND (env_type IS NULL OR env_type IN ('test','review','production')))
            OR (node_type<>'region' AND env_type IS NULL)
        ),
        CONSTRAINT chk_replica_node_ancestry CHECK (
			(node_type='game' AND game_id IS NULL AND region_id IS NULL)
			OR (node_type='region' AND COALESCE(game_id,0)>0 AND region_id IS NULL)
			OR (node_type='server_group' AND COALESCE(game_id,0)>0 AND COALESCE(region_id,0)>0)
		)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`
	const apps = `CREATE TABLE IF NOT EXISTS config_channel_app (
		id BIGINT PRIMARY KEY,game_id BIGINT NOT NULL,code VARCHAR(128) COLLATE utf8mb4_bin NOT NULL,
		name VARCHAR(128) NOT NULL,status TINYINT NOT NULL,config JSON NOT NULL,
		KEY idx_replica_apps_game (game_id)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`
	const bindings = `CREATE TABLE IF NOT EXISTS config_channel_app_binding (
		id BIGINT PRIMARY KEY,channel_app_id BIGINT NOT NULL,server_group_id BIGINT NOT NULL,
		status TINYINT NOT NULL,config JSON NOT NULL,effective_config JSON NOT NULL,
		UNIQUE KEY uq_replica_app_group (channel_app_id,server_group_id),
		KEY idx_replica_binding_group (server_group_id)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`
	const state = `CREATE TABLE IF NOT EXISTS config_sync_state (
		id TINYINT NOT NULL PRIMARY KEY,
		epoch CHAR(36) NULL,
		revision BIGINT NOT NULL,
		updated_at DATETIME(6) NOT NULL
	) ENGINE=InnoDB`
	for _, ddl := range []string{nodes, apps, bindings, state} {
		if _, err := s.DB.ExecContext(ctx, ddl); err != nil {
			return fmt.Errorf("initialize replica schema: %w", err)
		}
	}
	// 不对已有表做破坏性 ALTER；历史 V1/V2 数据库应先完成 Flyway V3。
	rows, err := s.DB.QueryContext(ctx, `SELECT game_id,region_id,env_type,node_type FROM config_node LIMIT 0`)
	if err != nil {
		return fmt.Errorf("obsolete replica schema: apply migrations through V5 first: %w", err)
	}
	if err := rows.Close(); err != nil { return err }
	var obsoleteColumns int
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.columns
        WHERE table_schema=DATABASE() AND table_name='config_node' AND column_name='server_group_id'`).Scan(&obsoleteColumns); err != nil {
		return fmt.Errorf("inspect replica node schema: %w", err)
	}
	if obsoleteColumns != 0 {
		return fmt.Errorf("obsolete replica schema: apply db/migration/V5__trim_node_group_id_replica.sql before startup")
	}
	if _, err := s.DB.ExecContext(ctx, `INSERT IGNORE INTO config_sync_state(id,epoch,revision,updated_at)
		VALUES(1,NULL,0,UTC_TIMESTAMP(6))`); err != nil {
		return err
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
func (s *Replica) Apply(ctx context.Context, expected, next State, nodes []remote.Node, deleted []int64, full bool, extras ...any) (bool, error) {
	if next.Epoch == "" || next.Revision < 0 {
		return false, fmt.Errorf("invalid target cursor")
	}
	for _, node := range nodes {
		if err := validateNode(node); err != nil {
			return false, err
		}
	}
	var apps []remote.ChannelApp
	var bindings []remote.Binding
	if len(extras)!=0 {
		if len(extras)!=2 { return false,fmt.Errorf("invalid channel snapshot") }
		var ok bool
		apps,ok=extras[0].([]remote.ChannelApp); if !ok { return false,fmt.Errorf("invalid channel apps") }
		bindings,ok=extras[1].([]remote.Binding); if !ok { return false,fmt.Errorf("invalid channel bindings") }
	}
	if full {
		for _, a:=range apps { if a.ID<=0 || a.GameID<=0 || a.Code=="" || !json.Valid([]byte(a.Config)) { return false,fmt.Errorf("invalid app snapshot") } }
		for _, b:=range bindings { if b.ID<=0 || b.ChannelAppID<=0 || b.ServerGroupID<=0 || !json.Valid([]byte(b.Config)) || !json.Valid([]byte(b.EffectiveConfig)) { return false,fmt.Errorf("invalid binding snapshot") } }
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
		if _,err:=tx.ExecContext(ctx,"DELETE FROM config_channel_app_binding"); err!=nil { return false,err }
		if _,err:=tx.ExecContext(ctx,"DELETE FROM config_channel_app"); err!=nil { return false,err }
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
	if full {
		for _,a:=range apps {
			if _,err:=tx.ExecContext(ctx,`INSERT INTO config_channel_app(id,game_id,code,name,status,config)
				VALUES(?,?,?,?,?,?)`,a.ID,a.GameID,a.Code,a.Name,a.Status,a.Config); err!=nil { return false,fmt.Errorf("store channel app %d: %w",a.ID,err) }
		}
		for _,b:=range bindings {
			if _,err:=tx.ExecContext(ctx,`INSERT INTO config_channel_app_binding(id,channel_app_id,server_group_id,status,config,effective_config)
				VALUES(?,?,?,?,?,?)`,b.ID,b.ChannelAppID,b.ServerGroupID,b.Status,b.Config,b.EffectiveConfig); err!=nil {
				return false,fmt.Errorf("store channel binding %d: %w",b.ID,err)
			}
		}
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
            case "test", "review", "production":
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

// ChannelBindings 返回指定区服组已同步的生效配置；仅供可信内部模块调用。
func (s *Replica) ChannelBindings(ctx context.Context,groupID int64) ([]remote.Binding,error) {
	if groupID<=0 { return nil,fmt.Errorf("invalid server group") }
	rows,err:=s.DB.QueryContext(ctx,`SELECT id,channel_app_id,server_group_id,status,config,effective_config
		FROM config_channel_app_binding WHERE server_group_id=? ORDER BY id`,groupID)
	if err!=nil { return nil,err }
	defer rows.Close()
	out:=make([]remote.Binding,0)
	for rows.Next() {
		var b remote.Binding
		if err:=rows.Scan(&b.ID,&b.ChannelAppID,&b.ServerGroupID,&b.Status,&b.Config,&b.EffectiveConfig); err!=nil { return nil,err }
		out=append(out,b)
	}
	return out,rows.Err()
}
