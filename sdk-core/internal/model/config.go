// Package model 定义 SDK 配置查询需要的数据库领域对象。
package model

import (
    "encoding/json"
    "errors"
)

var (
    ErrInvalidRequest = errors.New("配置查询请求不合法")
    ErrNotFound = errors.New("渠道包或环境绑定不存在或已禁用")
    ErrAmbiguous = errors.New("渠道包编码不是全局唯一")
    ErrInvalidConfig = errors.New("必需的响应配置不合法或缺失")
)

// ChannelApp 是已启用的渠道应用及其关联游戏配置快照。
type ChannelApp struct {
    ID int64
    GameID int64
    Code string
    Name string
    GameCode string
    GameConfig json.RawMessage
    GameSDKConfig json.RawMessage
    Config json.RawMessage
}

// BoundGroup 为目标环境实际绑定且启用的唯一区服组。
type BoundGroup struct {
    RegionCode string
    RegionConfig json.RawMessage
    RegionSDKConfig json.RawMessage
    ServerGroupCode string
    Config json.RawMessage
    SDKConfig json.RawMessage
}

// ConfigResponse 是公开 AppID 配置接口的固定响应结构，不包含游戏或平台密钥。
type ConfigResponse struct {
    SnKey   string  `json:"snKey"`
    ZoneKey string  `json:"zoneKey"`
    TsKey   *string `json:"tsKey,omitempty"`
}
