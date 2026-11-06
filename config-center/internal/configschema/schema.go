package configschema

import (
    "encoding/json"
    "errors"
    "fmt"
)

// Field 是基础配置预置字段的唯一元数据来源。
type Field struct {
    Key, Title, Description, Group, Placeholder string
    Secret bool
    Order int
}

var fields = map[string][]Field{
    "game": {},
    "region": {},
    "server_group": {},
    "channel_app": {
        {Key: "tsKey", Title: "TS Key", Description: "SDK Core 公开配置接口可返回该字段（可选）", Group: "公开配置", Placeholder: "请输入 TS Key", Order: 1},
        {Key: "appSecret", Title: "平台 App Secret", Description: "平台回调验签使用的服务端密钥", Group: "安全与签名", Placeholder: "请输入平台 App Secret", Secret: true, Order: 2},
    },
}

// SDK 专属配置不得被重新写入基础配置和同步快照。
var sdkOwned = map[string]string{
    "gameSecret": "game", "serviceAdapters": "game",
    "payNotifyUrl": "region", "userCheckUrl": "region",
    "roleQueryUrl": "region", "serverQueryUrl": "region", "roleControlUrl": "region",
}

// Schema 返回 JSON Schema 2020-12 格式，允许额外业务字段。
func Schema(nodeType string) (map[string]any, bool) {
    definitions, ok := fields[nodeType]
    if !ok || nodeType == "channel_app" { return nil, false }
    properties := make(map[string]any, len(definitions))
    for _, field := range definitions {
        item := map[string]any{
            "type": "string", "title": field.Title, "description": field.Description,
            "x-group": field.Group, "x-order": field.Order, "x-placeholder": field.Placeholder,
        }
        if field.Secret { item["writeOnly"] = true }
        properties[field.Key] = item
    }
    return map[string]any{
        "nodeType": nodeType, "schemaVersion": 1,
        "schema": map[string]any{
            "$schema": "https://json-schema.org/draft/2020-12/schema",
            "type": "object", "properties": properties,
            "required": []string{}, "additionalProperties": true,
        },
    }, true
}

// Validate 与 Schema 共用字段定义，额外校验 SDK 字段归属。
func Validate(nodeType, raw string) error {
    definitions, ok := fields[nodeType]
    if !ok { return fmt.Errorf("不支持的节点类型 %q", nodeType) }
    var object map[string]json.RawMessage
    if err := json.Unmarshal([]byte(raw), &object); err != nil || object == nil {
        return errors.New("配置必须是 JSON 对象")
    }
    for key := range object {
        if owner, exists := sdkOwned[key]; exists && owner == nodeType {
            return fmt.Errorf("%s 已迁移到 SDK 专属配置", key)
        }
    }
    for _, field := range definitions {
        value, exists := object[field.Key]
        if !exists { continue }
        var content string
        if string(value) == "null" || json.Unmarshal(value, &content) != nil {
            return fmt.Errorf("%s 必须是字符串", field.Key)
        }
    }
    return nil
}
