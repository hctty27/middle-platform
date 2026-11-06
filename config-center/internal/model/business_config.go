package model

import (
    "fmt"
    "github.com/hctty27/middle-platform/config-center/internal/configschema"
)

// 基础节点保留自定义 JSON；SDK 专属字段在 SDK Core 中管理。
type GameConfig struct { GameSecret *string `json:"gameSecret,omitempty"` }
type RegionConfig struct { PayNotifyURL *string `json:"payNotifyUrl,omitempty"` }
type ServerGroupConfig struct{}
type ChannelAppConfig struct {
    TsKey *string `json:"tsKey,omitempty"`
    AppSecret *string `json:"appSecret,omitempty"`
}

// ValidateBusinessConfig 使用与对外 Schema 同源的预置字段定义。
func ValidateBusinessConfig(nodeType, config string) error {
    if err := configschema.Validate(nodeType, config); err != nil {
        return fmt.Errorf("%w: %v", ErrInvalid, err)
    }
    return nil
}
