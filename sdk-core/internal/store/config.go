// Package store 提供配置中心权威 MySQL 的只读查询。
package store

import (
    "context"
    "database/sql"
    "encoding/json"
    "errors"
    "fmt"

    "github.com/hctty27/middle-platform/sdk-core/internal/model"
)

type Store struct {
    db *sql.DB
}

func NewStore(db *sql.DB) *Store {
    return &Store{db: db}
}

// configQuery 通过全局唯一的渠道 Code 一次获取当前环境的应用及完整区服配置。
// 应用、游戏、大区和区服组必须启用，且大区与区服组必须属于同一游戏。
const configQuery = `
SELECT
    a.id,
    a.game_id,
    a.code,
    a.name,
    a.config,
    g.code,
    r.code,
    r.config,
    sg.code,
    sg.config
FROM config_channel_app a
JOIN config_node g
    ON g.id = a.game_id
    AND g.node_type = 'game'
    AND g.status = 1
JOIN config_node sg
    ON sg.id = CASE ?
        WHEN 'prod' THEN a.prod_server_group_id
        WHEN 'test' THEN a.test_server_group_id
        WHEN 'review' THEN a.review_server_group_id
        ELSE NULL
    END
    AND sg.node_type = 'server_group'
    AND sg.status = 1
    AND sg.game_id = a.game_id
JOIN config_node r
    ON r.id = sg.region_id
    AND r.node_type = 'region'
    AND r.status = 1
    AND r.game_id = a.game_id
    AND r.env_type = ?
WHERE a.code = ?
    AND a.status = 1
LIMIT 1`

// Lookup 使用单条 SELECT 获取一致的应用、大区、区服组数据，避免二次查询和显式事务开销。
func (s *Store) Lookup(ctx context.Context, appID, env string) (model.ChannelApp, *model.BoundGroup, error) {
    var app model.ChannelApp
    var appJSON, regionJSON, groupJSON []byte
    group := &model.BoundGroup{}

    err := s.db.QueryRowContext(ctx, configQuery, env, env, appID).Scan(
        &app.ID,
        &app.GameID,
        &app.Code,
        &app.Name,
        &appJSON,
        &app.GameCode,
        &group.RegionCode,
        &regionJSON,
        &group.ServerGroupCode,
        &groupJSON,
    )
    if errors.Is(err, sql.ErrNoRows) {
        return model.ChannelApp{}, nil, model.ErrNotFound
    }
    if err != nil {
        return model.ChannelApp{}, nil, fmt.Errorf("query sdk configuration: %w", err)
    }

    app.Config = append(json.RawMessage(nil), appJSON...)
    group.RegionConfig = append(json.RawMessage(nil), regionJSON...)
    group.Config = append(json.RawMessage(nil), groupJSON...)
    return app, group, nil
}

// VerifySchema 复用实际查询验证所有必需表和字段，避免单独维护一份易过时的探测 SQL。
func (s *Store) VerifySchema(ctx context.Context) error {
    rows, err := s.db.QueryContext(ctx, configQuery, "prod", "prod", "__sdk_core_schema_probe__")
    if err != nil {
        return fmt.Errorf("verify sdk-core database schema: %w", err)
    }
    defer rows.Close()
    return rows.Err()
}
