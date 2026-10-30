// Package model 定义 SDK 配置查询的领域对象及公开协议。
package model

import (
	"encoding/json"
	"errors"
)

var (
	ErrInvalidRequest = errors.New("invalid configuration request")
	ErrNotFound       = errors.New("channel app not found or disabled")
	ErrAmbiguous      = errors.New("channel app code is not globally unique")
	ErrInvalidConfig  = errors.New("channel app config is not a JSON object")
)

// ChannelApp 是已关联游戏的渠道应用快照。
type ChannelApp struct {
	ID     int64
	GameID int64
	Config json.RawMessage
}

// BoundGroup 表示数据库中已启用的实际绑定。
type BoundGroup struct {
	RegionID        int64
	RegionCode      string
	ServerGroupCode string
}

// ServerGroup 只暴露区服组 Code。
type ServerGroup struct {
	Code string `json:"code"`
}

// Region 将同一大区下的已绑定区服组组合在一起。
type Region struct {
	Code         string        `json:"code"`
	ServerGroups []ServerGroup `json:"serverGroups"`
}

// ConfigData 是 /api/core/{env}/{appid} 的 data 部分。
type ConfigData struct {
	AppID   string          `json:"appid"`
	Env     string          `json:"env"`
	Config  json.RawMessage `json:"config"`
	Regions []Region        `json:"regions"`
}
