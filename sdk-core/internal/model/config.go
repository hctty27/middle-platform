// Package model 定义 SDK 配置查询需要的数据库领域对象。
package model

import (
    "encoding/json"
    "errors"
)

var (
    ErrInvalidRequest = errors.New("invalid configuration request")
    ErrNotFound = errors.New("channel app or environment binding not found or disabled")
    ErrAmbiguous = errors.New("channel app code is not globally unique")
    ErrInvalidConfig = errors.New("required response configuration is invalid or missing")
)

// ChannelApp 是已启用的渠道应用及其关联游戏配置快照。
type ChannelApp struct {
    ID int64
    GameID int64
    Code string
    Name string
    GameCode string
    Config json.RawMessage
}

// BoundGroup 为目标环境实际绑定且启用的唯一区服组。
type BoundGroup struct {
    RegionCode string
    RegionConfig json.RawMessage
    ServerGroupCode string
    Config json.RawMessage
}
