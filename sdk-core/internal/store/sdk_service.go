package store

import (
    "context"
    "database/sql"
    "encoding/json"
    "fmt"

    "github.com/hctty27/middle-platform/sdk-core/internal/model"
)

// LookupSDKService 查询某环境绑定的服务是否生效，仅在服务全局与渠道包双重开启时返回配置。
// 供后续事件上报、角色查询等业务使用；本模块不注册尚未实现的接口。
func (s *Store) LookupSDKService(ctx context.Context, appID, env, serviceCode string) (bool, json.RawMessage, error) {
    if appID == "" || serviceCode == "" ||
        (env != "prod" && env != "test" && env != "review") {
        return false, nil, model.ErrInvalidRequest
    }
    const query = `
SELECT ch.config
FROM sdk_service sv
JOIN sdk_service_channel ch ON ch.service_id=sv.id AND ch.env=? AND ch.enabled=1
JOIN config_channel_app a ON a.id=ch.channel_app_id AND a.code=? AND a.status=1
JOIN config_node g ON g.id=a.game_id AND g.node_type='game' AND g.status=1
JOIN config_node sg ON sg.id=CASE ?
  WHEN 'prod' THEN a.prod_server_group_id
  WHEN 'test' THEN a.test_server_group_id
  WHEN 'review' THEN a.review_server_group_id
END AND sg.node_type='server_group' AND sg.status=1 AND sg.game_id=a.game_id
JOIN config_node r ON r.id=sg.region_id AND r.node_type='region'
 AND r.status=1 AND r.game_id=a.game_id AND r.env_type=?
WHERE sv.code=? AND sv.enabled=1 LIMIT 1`
    var raw []byte
    err := s.db.QueryRowContext(ctx, query, env, appID, env, env, serviceCode).Scan(&raw)
    if err == sql.ErrNoRows { return false, nil, nil }
    if err != nil { return false, nil, fmt.Errorf("查询 SDK 服务开关失败: %w",err) }
    var object map[string]json.RawMessage
    if err := json.Unmarshal(raw,&object); err != nil || object == nil {
        return false,nil,model.ErrInvalidConfig
    }
    return true,append(json.RawMessage(nil),raw...),nil
}

// VerifySDKServiceSchema 检查事件上报依赖的服务开关表，不执行 DDL。
func (s *Store) VerifySDKServiceSchema(ctx context.Context) error {
    rows, err := s.db.QueryContext(ctx, `SELECT sv.id,sv.code,sv.enabled,
        ch.channel_app_id,ch.service_id,ch.env,ch.enabled,ch.config
        FROM sdk_service sv
        LEFT JOIN sdk_service_channel ch ON ch.service_id=sv.id LIMIT 0`)
    if err != nil {
        return fmt.Errorf("检查 SDK 服务开关表结构失败: %w", err)
    }
    defer rows.Close()
    return rows.Err()
}
