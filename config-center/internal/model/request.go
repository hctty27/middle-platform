package model

// CreateNodeRequest 定义创建配置节点的请求参数。
type CreateNodeRequest struct {
	ParentID int64   `json:"parentId"`
	NodeType string  `json:"nodeType"`
	// 只有创建大区时允许设置环境类型，其他层级必须为空。
	EnvType  *string `json:"envType"`
	Code     string  `json:"code"`
	Name     string  `json:"name"`
	Status   *int    `json:"status"`
	Config   string  `json:"config"`
	Remark   *string `json:"remark"`
}

// UpdateNodeRequest 定义修改节点允许更新的字段。
type UpdateNodeRequest struct {
	// 环境分类由服务端执行节点类型与修改约束校验。
	EnvType *string `json:"envType"`
	Name   string  `json:"name"`
	Status *int    `json:"status"`
	Config string  `json:"config"`
	Remark *string `json:"remark"`
}

// CreateSubscriberRequest 定义回调订阅请求参数。
type CreateSubscriberRequest struct {
	Code        string `json:"code"`
	CallbackURL string `json:"callbackUrl"`
	// 回调密钥只用于服务端签名，不应通过查询接口返回。
	Secret      string `json:"secret"`
}
