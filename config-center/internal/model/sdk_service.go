package model

import "encoding/json"

// SDKService 定义 SDK 功能及全局开关。
type SDKService struct {
	ID          int64         `json:"id"`
	Code        string        `json:"code"`
	Name        string        `json:"name"`
	Description string        `json:"description"`
	Enabled     bool          `json:"enabled"`
	CreatedAt   LocalDateTime `json:"createdAt"`
	UpdatedAt   LocalDateTime `json:"updatedAt"`
}

// SDKChannelService 表示渠道包在指定环境的服务设置，缺省绑定视为关闭。
type SDKChannelService struct {
	ServiceID        int64           `json:"serviceId"`
	Code             string          `json:"code"`
	Name             string          `json:"name"`
	Description      string          `json:"description"`
	GlobalEnabled    bool            `json:"globalEnabled"`
	Enabled          bool            `json:"enabled"`
	EffectiveEnabled bool            `json:"effectiveEnabled"`
	Config           json.RawMessage `json:"config"`
}

type UpdateSDKServiceRequest struct {
	Enabled *bool `json:"enabled"`
}

type UpsertSDKChannelServiceRequest struct {
	Enabled *bool           `json:"enabled"`
	Config  json.RawMessage `json:"config"`
}
