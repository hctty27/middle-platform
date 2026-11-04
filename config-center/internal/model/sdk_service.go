package model

import "encoding/json"

// SDKService 定义全局服务及作用范围，已有绑定后 scope_type 不可直接修改。
type SDKService struct {
    ID int64 `json:"id"`
    Code string `json:"code"`
    Name string `json:"name"`
    ScopeType string `json:"scopeType"`
    Description string `json:"description"`
    Enabled bool `json:"enabled"`
    CreatedAt LocalDateTime `json:"createdAt"`
    UpdatedAt LocalDateTime `json:"updatedAt"`
}

// SDKBindingService 是指定目标和环境的服务配置，缺省记录视为关闭。
type SDKBindingService struct {
    ServiceID int64 `json:"serviceId"`
    Code string `json:"code"`
    Name string `json:"name"`
    Description string `json:"description"`
    GlobalEnabled bool `json:"globalEnabled"`
    Enabled bool `json:"enabled"`
    EffectiveEnabled bool `json:"effectiveEnabled"`
    Config json.RawMessage `json:"config"`
}

// 保留现有模型别名，管理端响应契约保持不变。
type SDKChannelService = SDKBindingService

type UpdateSDKServiceRequest struct {
    Enabled *bool `json:"enabled"`
}

type UpsertSDKChannelServiceRequest struct {
    Enabled *bool `json:"enabled"`
    Config json.RawMessage `json:"config"`
}
