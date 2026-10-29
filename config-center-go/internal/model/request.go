package model

// CreateNodeRequest 定义创建配置节点的请求参数。
type CreateNodeRequest struct {
	ParentID int64   `json:"parentId"`
	NodeType string  `json:"nodeType"`
	Code     string  `json:"code"`
	Name     string  `json:"name"`
	Status   *int    `json:"status"`
	Config   string  `json:"config"`
	Remark   *string `json:"remark"`
}

// UpdateNodeRequest 定义修改节点允许更新的字段。
type UpdateNodeRequest struct {
	Name   string  `json:"name"`
	Status *int    `json:"status"`
	Config string  `json:"config"`
	Remark *string `json:"remark"`
}

// CreateSubscriberRequest 定义回调订阅请求参数。
type CreateSubscriberRequest struct {
	Code        string `json:"code"`
	CallbackURL string `json:"callbackUrl"`
	Secret      string `json:"secret"`
}
