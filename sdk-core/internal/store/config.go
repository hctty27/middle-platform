// Package store 提供只读 MySQL 查询，不维护配置副本。
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/hctty27/middle-platform/sdk-core/internal/model"
)

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

const appQuery = `
SELECT a.id, a.game_id, a.status, a.config, g.status
FROM config_channel_app a
JOIN config_node g ON g.id = a.game_id AND g.node_type = 'game'
WHERE a.code = ?
LIMIT 2`

const boundGroupQuery = `
SELECT r.id, r.code, sg.code
FROM config_channel_app a
JOIN config_node sg ON sg.id = CASE ?
    WHEN 'prod' THEN a.prod_server_group_id
    WHEN 'test' THEN a.test_server_group_id
    WHEN 'review' THEN a.review_server_group_id
    ELSE NULL END
JOIN config_node r ON r.id = sg.region_id
WHERE a.id = ?
  AND sg.node_type = 'server_group'
  AND sg.status = 1
  AND sg.game_id = ?
  AND r.node_type = 'region'
  AND r.status = 1
  AND r.game_id = ?
  AND r.env_type = ?
LIMIT 1`

// Lookup 在同一只读一致性事务中查询应用与指定环境下的实际绑定。
// 未绑定的区服组及其空大区不会进入结果。
func (s *Store) Lookup(ctx context.Context, appID, env string) (model.ChannelApp, []model.BoundGroup, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return model.ChannelApp{}, nil, fmt.Errorf("begin config query: %w", err)
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, appQuery, appID)
	if err != nil {
		return model.ChannelApp{}, nil, fmt.Errorf("query channel app: %w", err)
	}
	var app model.ChannelApp
	var appStatus, gameStatus, count int
	for rows.Next() {
		var config []byte
		if err := rows.Scan(&app.ID, &app.GameID, &appStatus, &config, &gameStatus); err != nil {
			rows.Close()
			return model.ChannelApp{}, nil, fmt.Errorf("scan channel app: %w", err)
		}
		app.Config = append(json.RawMessage(nil), config...)
		count++
	}
	scanErr := rows.Err()
	rows.Close()
	if scanErr != nil {
		return model.ChannelApp{}, nil, fmt.Errorf("iterate channel apps: %w", scanErr)
	}
	if count > 1 {
		return model.ChannelApp{}, nil, model.ErrAmbiguous
	}
	if count == 0 || appStatus != 1 || gameStatus != 1 {
		return model.ChannelApp{}, nil, model.ErrNotFound
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(app.Config, &obj); err != nil || obj == nil {
		return model.ChannelApp{}, nil, model.ErrInvalidConfig
	}

	bindings, err := tx.QueryContext(ctx, boundGroupQuery, env, app.ID, app.GameID, app.GameID, env)
	if err != nil {
		return model.ChannelApp{}, nil, fmt.Errorf("query bound server groups: %w", err)
	}
	groups := make([]model.BoundGroup, 0)
	for bindings.Next() {
		var group model.BoundGroup
		if err := bindings.Scan(&group.RegionID, &group.RegionCode, &group.ServerGroupCode); err != nil {
			bindings.Close()
			return model.ChannelApp{}, nil, fmt.Errorf("scan bound server group: %w", err)
		}
		groups = append(groups, group)
	}
	scanErr = bindings.Err()
	bindings.Close()
	if scanErr != nil {
		return model.ChannelApp{}, nil, fmt.Errorf("iterate bound server groups: %w", scanErr)
	}
	if err := tx.Commit(); err != nil {
		return model.ChannelApp{}, nil, fmt.Errorf("commit config read: %w", err)
	}
	return app, groups, nil
}

// VerifySchema 在启动时验证查询依赖的业务表和字段存在。
func (s *Store) VerifySchema(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `
SELECT a.code, a.config, a.prod_server_group_id, a.test_server_group_id, a.review_server_group_id, sg.region_id, r.env_type
FROM config_channel_app a
LEFT JOIN config_node sg ON sg.id = a.prod_server_group_id
LEFT JOIN config_node r ON r.id = sg.region_id
WHERE 1 = 0`)
	if err != nil {
		return fmt.Errorf("verify sdk-core database schema: %w", err)
	}
	return rows.Close()
}
