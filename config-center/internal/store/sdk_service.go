package store

import (
    "bytes"
    "context"
    "database/sql"
    "encoding/json"
    "io"
    "time"

    "github.com/hctty27/middle-platform/config-center/internal/model"
)

const sdkServiceColumns = "id,code,name,description,enabled,sort_order,created_at,updated_at"

// SDKServices 返回 SDK 支持的全局服务目录。
func (s *Store) SDKServices(ctx context.Context) ([]model.SDKService, error) {
    rows, err := s.DB.QueryContext(ctx, "SELECT "+sdkServiceColumns+" FROM sdk_service ORDER BY sort_order,id")
    if err != nil { return nil, err }
    defer rows.Close()
    items := make([]model.SDKService, 0)
    for rows.Next() {
        var item model.SDKService
        var enabled int
        var created, updated time.Time
        if err := rows.Scan(&item.ID, &item.Code, &item.Name, &item.Description, &enabled, &item.SortOrder, &created, &updated); err != nil {
            return nil, err
        }
        item.Enabled = enabled == 1
        item.CreatedAt, item.UpdatedAt = model.LocalDateTime(created), model.LocalDateTime(updated)
        items = append(items, item)
    }
    return items, rows.Err()
}

// UpdateSDKService 只允许修改全局启用状态；服务编码由代码及初始化 SQL 管理。
func (s *Store) UpdateSDKService(ctx context.Context, id int64, enabled bool) error {
    if id <= 0 { return ErrInvalid }
    result, err := s.DB.ExecContext(ctx,
        "UPDATE sdk_service SET enabled=?,updated_at=UTC_TIMESTAMP(3) WHERE id=?", enabled, id)
    if err != nil { return databaseError(err) }
    affected, err := result.RowsAffected()
    if err != nil { return err }
    if affected > 0 { return nil }
    var exists int
    return s.DB.QueryRowContext(ctx, "SELECT 1 FROM sdk_service WHERE id=?", id).Scan(&exists)
}

func validSDKEnv(env string) bool {
    return env == "prod" || env == "test" || env == "review"
}

// SDKChannelServices 返回整个服务目录，其中无渠道覆盖记录的服务默认关闭。
func (s *Store) SDKChannelServices(ctx context.Context, appID int64, env string) ([]model.SDKChannelService, error) {
    if appID <= 0 || !validSDKEnv(env) { return nil, ErrInvalid }
    var exists int64
    if err := s.DB.QueryRowContext(ctx,
        "SELECT id FROM config_channel_app WHERE id=?", appID).Scan(&exists); err != nil {
        return nil, err
    }
    rows, err := s.DB.QueryContext(ctx, `
SELECT service.id,service.code,service.name,service.description,service.enabled,
       COALESCE(channel.enabled,0),COALESCE(channel.config,JSON_OBJECT())
FROM sdk_service service
LEFT JOIN sdk_service_channel channel
  ON channel.service_id=service.id AND channel.channel_app_id=? AND channel.env=?
ORDER BY service.sort_order,service.id`, appID, env)
    if err != nil { return nil, err }
    defer rows.Close()
    items := make([]model.SDKChannelService, 0)
    for rows.Next() {
        var item model.SDKChannelService
        var globalEnabled, channelEnabled int
        var config []byte
        if err := rows.Scan(&item.ServiceID, &item.Code, &item.Name, &item.Description,
            &globalEnabled, &channelEnabled, &config); err != nil { return nil, err }
        item.GlobalEnabled = globalEnabled == 1
        item.Enabled = channelEnabled == 1
        item.EffectiveEnabled = item.GlobalEnabled && item.Enabled
        item.Config = append(json.RawMessage(nil), config...)
        items = append(items, item)
    }
    return items, rows.Err()
}

// UpsertSDKChannelService 原子保存某渠道包在指定环境的开关和专属参数。
func (s *Store) UpsertSDKChannelService(ctx context.Context, appID, serviceID int64, env string,
    enabled bool, config json.RawMessage) error {
    if appID <= 0 || serviceID <= 0 || !validSDKEnv(env) || len(config) == 0 || len(config) > 64<<10 {
        return ErrInvalid
    }
    decoder := json.NewDecoder(bytes.NewReader(config))
    var object map[string]json.RawMessage
    if err := decoder.Decode(&object); err != nil || object == nil { return ErrInvalid }
    if decoder.Decode(new(any)) != io.EOF { return ErrInvalid }
    _, err := s.DB.ExecContext(ctx, `
INSERT INTO sdk_service_channel (channel_app_id,service_id,env,enabled,config)
VALUES (?,?,?,?,?)
ON DUPLICATE KEY UPDATE enabled=VALUES(enabled),config=VALUES(config),updated_at=UTC_TIMESTAMP(3)`,
        appID, serviceID, env, enabled, string(config))
    return databaseError(err)
}

// SDKServiceChannelAllowed 供后续 SDK Core 业务使用，管理端不把开关当作权限验证。
func (s *Store) SDKServiceChannelAllowed(ctx context.Context, appCode, env, serviceCode string) (bool, error) {
    if appCode == "" || serviceCode == "" || !validSDKEnv(env) { return false, ErrInvalid }
    var enabled int
    err := s.DB.QueryRowContext(ctx, `
SELECT IF(service.enabled=1 AND channel.enabled=1 AND app.status=1,1,0)
FROM config_channel_app app
JOIN sdk_service_channel channel ON channel.channel_app_id=app.id AND channel.env=?
JOIN sdk_service service ON service.id=channel.service_id AND service.code=?
WHERE app.code=?`,env,serviceCode,appCode).Scan(&enabled)
    if err == sql.ErrNoRows { return false,nil }
    return enabled==1,err
}
