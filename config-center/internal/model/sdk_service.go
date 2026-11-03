package model

import "encoding/json"

// SDKService 定义 SDK 的全局业务能力和总开关。
type SDKService struct {
    ID int64 `json:"id"`
    Code string `json:"code"`
    Name string `json:"name"`
    Description string `json:"description"`
    Enabled bool `json:"enabled"`
    SortOrder int `json:"sortOrder"`
    CreatedAt LocalDateTime `json:"createdAt"`
    UpdatedAt LocalDateTime `json:"updatedAt"`
}

// SDKChannelService 是渠道包指定环境的服务配置，缺省记录表示关闭。
type SDKChannelService struct {
    ServiceID int64 `json:"serviceId"`
    Code string `json:"code"`
    Name string `json:"name"`
    Description string `json:"description"`
    GlobalEnabled bool `json:"globalEnabled"`
    Enabled bool `json:"enabled"`
    EffectiveEnabled bool `json:"effectiveEnabled"`
    Config json.RawMessage `json:"config"`
}

type UpdateSDKServiceRequest struct {
    Enabled *bool `json:"enabled"`
}

type UpsertSDKChannelServiceRequest struct {
    Enabled *bool `json:"enabled"`
    Config json.RawMessage `json:"config"`
}
