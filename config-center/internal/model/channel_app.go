package model

// ChannelApp 是游戏级渠道应用，独立于大区和区服组。
type ChannelApp struct {
	ID int64 `json:"id"`
	GameID int64 `json:"gameId"`
	Code string `json:"code"`
	Name string `json:"name"`
	Status int `json:"status"`
	Config string `json:"config"`
	Remark *string `json:"remark"`
	CreatedAt LocalDateTime `json:"createdAt"`
	UpdatedAt LocalDateTime `json:"updatedAt"`
}

// ChannelAppBinding 是应用与区服组的关联。覆盖 JSON 只存差异字段。
type ChannelAppBinding struct {
	ID int64 `json:"id"`
	ChannelAppID int64 `json:"channelAppId"`
	ServerGroupID int64 `json:"serverGroupId"`
	Status int `json:"status"`
	Config string `json:"config"`
	EffectiveConfig string `json:"effectiveConfig"`
	CreatedAt LocalDateTime `json:"createdAt"`
	UpdatedAt LocalDateTime `json:"updatedAt"`
}

type CreateChannelAppRequest struct {
	GameID int64 `json:"gameId"`
	Code string `json:"code"`
	Name string `json:"name"`
	Status *int `json:"status"`
	Config string `json:"config"`
	Remark *string `json:"remark"`
}
type UpdateChannelAppRequest struct {
	Name string `json:"name"`
	Status *int `json:"status"`
	Config string `json:"config"`
	Remark *string `json:"remark"`
}
type CreateChannelAppBindingRequest struct {
	ServerGroupID int64 `json:"serverGroupId"`
	Status *int `json:"status"`
	Config string `json:"config"`
}
type UpdateChannelAppBindingRequest struct {
	Status *int `json:"status"`
	Config string `json:"config"`
}
