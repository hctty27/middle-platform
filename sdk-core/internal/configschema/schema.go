package configschema

import (
    "encoding/json"
    "errors"
    "fmt"
    "net/url"
    "strings"
)

// Field 同时用于 SDK 配置的 Schema 输出与写入校验。
type Field struct {
    Key, Title, Description, Group, Placeholder, Format string
    Secret bool
    Order int
}

var fields = map[string][]Field{
    "game": {
        {Key: "gameSecret", Title: "游戏主密钥", Description: "游戏服与 SDK 共用的服务端签名密钥", Group: "安全与签名", Secret: true, Order: 1},
        {Key: "serviceAdapters", Title: "服务适配器", Description: "留空时使用标准适配器，只有特殊游戏需要指定", Group: "服务适配器", Order: 2},
    },
    "region": {
        {Key: "payNotifyUrl", Title: "支付通知地址", Description: "SDK Core 支付成功后通知游戏服的完整地址", Group: "支付配置", Format: "uri", Placeholder: "https://game.example.com/pay/notify", Order: 1},
        {Key: "userCheckUrl", Title: "用户核验地址", Description: "SDK Service 核验用户的游戏服接口地址", Group: "用户与角色", Format: "uri", Order: 2},
        {Key: "roleQueryUrl", Title: "角色查询地址", Description: "SDK Service 查询游戏角色的接口地址", Group: "用户与角色", Format: "uri", Order: 3},
        {Key: "serverQueryUrl", Title: "区服查询地址", Description: "SDK Service 查询游戏区服的接口地址", Group: "区服接口", Format: "uri", Order: 4},
        {Key: "roleControlUrl", Title: "封解禁地址", Description: "SDK Service 封号、解封、禁言、解禁言接口地址", Group: "用户与角色", Format: "uri", Order: 5},
    },
    "server_group": {},
    "channel_app": {
        {Key: "tsKey", Title: "TS Key", Description: "SDK Core 公开配置接口返回的可选字段", Group: "公开配置", Order: 1},
        {Key: "appSecret", Title: "平台 App Secret", Description: "平台回调验签使用的服务端密钥", Group: "安全与签名", Secret: true, Order: 2},
    },
}

var owners = func() map[string]string {
    out := make(map[string]string)
    for kind, list := range fields {
        for _, field := range list { out[field.Key] = kind }
    }
    return out
}()

var adapterNames = map[string]string{
    "user_check": "用户核验", "role_query": "角色查询",
    "server_query": "区服查询", "role_control": "封解禁",
}

// Schema 返回字段描述，适配器对象同时允许额外的字符串键。
func Schema(nodeType string) (map[string]any, bool) {
    definitions, ok := fields[nodeType]
    if !ok { return nil, false }
    properties := make(map[string]any, len(definitions))
    for _, field := range definitions {
        item := map[string]any{
            "type": "string", "title": field.Title, "description": field.Description,
            "x-group": field.Group, "x-order": field.Order, "x-placeholder": field.Placeholder,
        }
        if field.Secret { item["writeOnly"] = true }
        if field.Format != "" {
            item["format"] = field.Format
            item["pattern"] = "^https?://"
        }
        if field.Key == "serviceAdapters" {
            nested := make(map[string]any, len(adapterNames))
            order := map[string]int{"user_check": 1, "role_query": 2, "server_query": 3, "role_control": 4}
            for key, title := range adapterNames {
                nested[key] = map[string]any{"type": "string", "title": title + "适配器",
                    "description": "不填写则使用 standard", "default": "standard",
                    "x-order": order[key], "x-placeholder": "留空默认 standard"}
            }
            item["type"] = "object"
            item["properties"] = nested
            item["additionalProperties"] = map[string]any{"type": "string"}
        }
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

// Validate 严格校验已知字段类型、字段归属及 HTTP 地址，未知字段原样保留。
func Validate(nodeType, raw string) error {
    definitions, ok := fields[nodeType]
    if !ok { return fmt.Errorf("不支持的节点类型 %q", nodeType) }
    var object map[string]json.RawMessage
    if err := json.Unmarshal([]byte(raw), &object); err != nil || object == nil {
        return errors.New("配置必须是 JSON 对象")
    }
    for key := range object {
        if owner, known := owners[key]; known && owner != nodeType {
            return fmt.Errorf("%s 不属于 %s 节点", key, nodeType)
        }
    }
    for _, field := range definitions {
        value, exists := object[field.Key]
        if !exists { continue }
        if field.Key == "serviceAdapters" {
            var adapters map[string]json.RawMessage
            if err := json.Unmarshal(value, &adapters); err != nil || adapters == nil {
                return errors.New("serviceAdapters 必须是 JSON 对象")
            }
            for key, item := range adapters {
                var name string
                if string(item) == "null" || json.Unmarshal(item, &name) != nil {
                    return fmt.Errorf("serviceAdapters.%s 必须是字符串", key)
                }
            }
            continue
        }
        var content string
        if string(value) == "null" || json.Unmarshal(value, &content) != nil {
            return fmt.Errorf("%s 必须是字符串", field.Key)
        }
        if field.Format == "uri" && strings.TrimSpace(content) != "" {
            parsed, err := url.Parse(content)
            if err != nil || parsed == nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" {
                return fmt.Errorf("%s 必须是完整的 HTTP/HTTPS 地址", field.Key)
            }
        }
    }
    return nil
}
