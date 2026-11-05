// Package store 只读查询 config_center 权威数据库，不进行数据库迁移。
package store

import (
    "context"
    "database/sql"
    "encoding/json"
    "errors"
    "fmt"
    "net/url"
    "strings"

    "github.com/hctty27/middle-platform/sdk-service/internal/adapter"
    "github.com/hctty27/middle-platform/sdk-service/internal/model"
)

type Store struct { db *sql.DB }
func New(db *sql.DB) *Store { return &Store{db: db} }

// 单次查询通过 AppID 与环境定位游戏、配置和渠道服务开关。
// 对不正确的环境绑定直接返回无记录，不能通过跨环境回退查询配置。
const routeQuery = `
SELECT a.config, g.code, COALESCE(gs.config, JSON_OBJECT()), COALESCE(rs.config, JSON_OBJECT()),
    COALESCE(sv.enabled, 0), COALESCE(ch.enabled, 0)
FROM sdk_channel_app a
JOIN config_node g ON g.id=a.game_id
    AND g.node_type='game' AND g.status=1
LEFT JOIN sdk_node_config gs ON gs.node_id=g.id
JOIN config_node sg ON sg.id=CASE ?
    WHEN 'prod' THEN a.prod_server_group_id
    WHEN 'test' THEN a.test_server_group_id
    WHEN 'review' THEN a.review_server_group_id
    ELSE NULL END
    AND sg.node_type='server_group' AND sg.game_id=a.game_id AND sg.status=1
JOIN config_node r ON r.id=sg.region_id
    AND r.node_type='region' AND r.game_id=a.game_id
    AND r.status=1 AND r.env_type=?
LEFT JOIN sdk_node_config rs ON rs.node_id=r.id
LEFT JOIN sdk_service sv ON sv.code=?
LEFT JOIN sdk_service_channel ch ON ch.service_id=sv.id
    AND ch.channel_app_id=a.id AND ch.env=?
WHERE a.code=? AND a.status=1`

func (s *Store) VerifySchema(ctx context.Context) error {
    rows, err := s.db.QueryContext(ctx, routeQuery+" LIMIT 0", "prod", "prod", "user_check", "prod", "__probe__")
    if err != nil { return fmt.Errorf("核验功能依赖的数据库结构不完整: %w", err) }
    if err := rows.Close(); err != nil { return err }
    for _, entry := range []struct{ code, script string }{
        {"user_check", "add_sdk_user_check.sql"},
        {"role_query", "add_sdk_role_query.sql"},
        {"server_query", "add_sdk_server_query.sql"},
        {"role_control", "add_sdk_role_control.sql"},
        {"event_report", "add_sdk_services.sql"},
        {"subscribe_message", "add_sdk_subscribe_message.sql"},
    } {
        var id int64
        err = s.db.QueryRowContext(ctx, "SELECT id FROM sdk_service WHERE code=?", entry.code).Scan(&id)
        if errors.Is(err, sql.ErrNoRows) {
            return fmt.Errorf("未安装 %s 服务目录，请先手动执行 %s", entry.code, entry.script)
        }
        if err != nil { return err }
    }
    return nil
}

func (s *Store) Lookup(ctx context.Context, appID, env string) (model.Target, error) {
    return s.LookupService(ctx, appID, env, "user_check")
}

func (s *Store) LookupService(ctx context.Context, appID, env, serviceCode string) (model.Target, error) {
    if serviceCode != "user_check" && serviceCode != "role_query" && serviceCode != "server_query" && serviceCode != "role_control" &&
        serviceCode != "event_report" && serviceCode != "subscribe_message" {
        return model.Target{}, model.ErrInvalidConfig
    }
    if env != "prod" && env != "test" && env != "review" { return model.Target{}, model.ErrNotFound }
    var appJSON, gameJSON, regionJSON []byte
    var gameCode string
    var globalEnabled, channelEnabled int
    err := s.db.QueryRowContext(ctx, routeQuery, env, env, serviceCode, env, appID).Scan(
        &appJSON, &gameCode, &gameJSON, &regionJSON, &globalEnabled, &channelEnabled,
    )
    if errors.Is(err, sql.ErrNoRows) { return model.Target{}, model.ErrNotFound }
    if err != nil { return model.Target{}, fmt.Errorf("查询 SDK 服务路由失败: %w", err) }
    if globalEnabled != 1 || channelEnabled != 1 { return model.Target{}, model.ErrDisabled }

    var app struct { AppSecret string `json:"appSecret"` }
    var game struct {
        GameSecret string `json:"gameSecret"`
        ServiceAdapters map[string]string `json:"serviceAdapters"`
    }
    var region struct {
        UserCheckURL string `json:"userCheckUrl"`
        RoleQueryURL string `json:"roleQueryUrl"`
        ServerQueryURL string `json:"serverQueryUrl"`
        RoleControlURL string `json:"roleControlUrl"`
    }
    if json.Unmarshal(appJSON, &app) != nil || json.Unmarshal(gameJSON, &game) != nil ||
        json.Unmarshal(regionJSON, &region) != nil {
        return model.Target{}, model.ErrInvalidConfig
    }
    if strings.TrimSpace(app.AppSecret) == "" || strings.TrimSpace(game.GameSecret) == "" {
        return model.Target{}, model.ErrInvalidConfig
    }
    adapterName := adapter.NameForService(game.ServiceAdapters, serviceCode)
    result := model.Target{
        AppID: appID, Env: env, GameCode: gameCode,
        AdapterName: adapterName, AppSecret: app.AppSecret, GameSecret: game.GameSecret,
    }
    // 游戏向平台发起的业务无需配置 region 游戏回调地址。
    if serviceCode == "event_report" || serviceCode == "subscribe_message" {
        return result, nil
    }
    targetURL := strings.TrimSpace(region.UserCheckURL)
    switch serviceCode {
    case "role_query":
        targetURL = strings.TrimSpace(region.RoleQueryURL)
    case "server_query":
        targetURL = strings.TrimSpace(region.ServerQueryURL)
    case "role_control":
        targetURL = strings.TrimSpace(region.RoleControlURL)
    }
    u, err := url.Parse(targetURL)
    if err != nil || (u.Scheme != "http" && u.Scheme != "https") ||
        u.Hostname() == "" || u.User != nil || u.Fragment != "" {
        return model.Target{}, model.ErrInvalidConfig
    }
    switch serviceCode {
    case "user_check":
        result.UserCheckURL = targetURL
    case "role_query":
        result.RoleQueryURL = targetURL
    case "server_query":
        result.ServerQueryURL = targetURL
    case "role_control":
        result.RoleControlURL = targetURL
    }
    return result, nil
}
