package model

import (
	"encoding/json"
	"fmt"
)

// 四类业务配置仍存储在各节点的 config JSON 中，字段为可选值。
// 指针用于区分字段缺失和空字符串；未知 JSON 字段不得因表单保存而丢失。
type GameConfig struct {
	GameSecret *string `json:"gameSecret,omitempty"`
}

type RegionConfig struct {
	PayNotifyURL *string `json:"payNotifyUrl,omitempty"`
}

type ServerGroupConfig struct{}

type ChannelAppConfig struct {
	TsKey     *string `json:"tsKey,omitempty"`
	AppSecret *string `json:"appSecret,omitempty"`
}

var businessConfigKeys = map[string][]string{
	"game":         {"gameSecret"},
	"region":       {"payNotifyUrl"},
	"server_group": {},
	"channel_app":  {"tsKey", "appSecret"},
}

// ValidateBusinessConfig 检查结构化字段的类型，允许任意额外 JSON 字段。
// 不重新序列化配置，确保兼容既有的自定义字段与 SDK Core 映射路径。
func ValidateBusinessConfig(nodeType, config string) error {
	keys, ok := businessConfigKeys[nodeType]
	if !ok {
		return fmt.Errorf("%w: unknown config type %q", ErrInvalid, nodeType)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal([]byte(config), &object); err != nil || object == nil {
		return fmt.Errorf("%w: config must be a JSON object", ErrInvalid)
	}
	for _, key := range keys {
		value, exists := object[key]
		if !exists {
			continue
		}
		var content string
		if string(value) == "null" || json.Unmarshal(value, &content) != nil {
			return fmt.Errorf("%w: %s must be a string", ErrInvalid, key)
		}
	}
	return nil
}
