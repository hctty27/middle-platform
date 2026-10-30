package model

// ChannelApp 统一配置三个环境，每个环境最多绑定一个区服组。
type ChannelApp struct {
    ID int64 `json:"id"`
    GameID int64 `json:"gameId"`
    Code string `json:"code"`
    Name string `json:"name"`
    Status int `json:"status"`
    Config string `json:"config"`
    Remark *string `json:"remark"`
    ProdServerGroupID *int64 `json:"prodServerGroupId"`
    TestServerGroupID *int64 `json:"testServerGroupId"`
    ReviewServerGroupID *int64 `json:"reviewServerGroupId"`
    CreatedAt LocalDateTime `json:"createdAt"`
    UpdatedAt LocalDateTime `json:"updatedAt"`
}

// ServerGroupOption 为管理端环境下拉框提供路径信息。
type ServerGroupOption struct {
    ID int64 `json:"id"`
    Name string `json:"name"`
    Code string `json:"code"`
    RegionName string `json:"regionName"`
}

// ServerGroupBinding 由应用的三个区服组 ID 推导，不再维护独立的状态和覆盖配置。
type ServerGroupBinding struct {
    ChannelAppID int64 `json:"channelAppId"`
    ChannelAppName string `json:"channelAppName"`
    ChannelAppCode string `json:"channelAppCode"`
    ServerGroupID int64 `json:"serverGroupId"`
    Status int `json:"status"`
    Config string `json:"config"`
}

type CreateChannelAppRequest struct {
    GameID int64 `json:"gameId"`
    Code string `json:"code"`
    Name string `json:"name"`
    Status *int `json:"status"`
    Config string `json:"config"`
    Remark *string `json:"remark"`
    ProdServerGroupID *int64 `json:"prodServerGroupId"`
    TestServerGroupID *int64 `json:"testServerGroupId"`
    ReviewServerGroupID *int64 `json:"reviewServerGroupId"`
}

type UpdateChannelAppRequest struct {
    Name string `json:"name"`
    Status *int `json:"status"`
    Config string `json:"config"`
    Remark *string `json:"remark"`
    ProdServerGroupID *int64 `json:"prodServerGroupId"`
    TestServerGroupID *int64 `json:"testServerGroupId"`
    ReviewServerGroupID *int64 `json:"reviewServerGroupId"`
}
