package model

// CreateNodeRequest 定义创建配置节点的请求参数。
type CreateNodeRequest struct {
	ParentID int64  `json:"parentId"`
	NodeType string `json:"nodeType"`
	// 只有创建大区时允许设置环境类型，其他层级必须为空。
	EnvType *string `json:"envType"`
	// 未传时创建游戏默认国内；其他节点不能设置。
	MarketType *string `json:"marketType"`
	Code       string  `json:"code"`
	Name       string  `json:"name"`
	Status     *int    `json:"status"`
	Config     string  `json:"config"`
	Remark     *string `json:"remark"`
}

// UpdateNodeRequest 定义修改节点允许更新的字段。
type UpdateNodeRequest struct {
	// 环境分类由服务端执行节点类型与修改约束校验。
	EnvType *string `json:"envType"`
	// 未传时保留游戏当前运营地区。
	MarketType *string `json:"marketType"`
	Name       string  `json:"name"`
	Status     *int    `json:"status"`
	Config     string  `json:"config"`
	Remark     *string `json:"remark"`
}

// CreateSubscriberRequest 定义回调订阅请求参数。
type CreateSubscriberRequest struct {
	Code        string `json:"code"`
	CallbackURL string `json:"callbackUrl"`
	// 接入端独立签名密钥用于双向同步认证，不应通过查询接口返回。
	Secret string `json:"secret"`
}


// UpdateSubscriberRequest 定义修改订阅者允许更新的字段。
type UpdateSubscriberRequest struct {
	CallbackURL string `json:"callbackUrl"`
	// 密钥留空时保留当前密钥；密钥不会通过查询接口回显。
	Secret string `json:"secret"`
}

// SetSubscriberEnabledRequest 定义订阅者启停请求。
type SetSubscriberEnabledRequest struct {
	Enabled *bool `json:"enabled"`
}
