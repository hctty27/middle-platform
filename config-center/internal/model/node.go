// Package model 定义节点、订阅者以及配置同步数据模型。
package model

import (
	"encoding/json"
	"errors"
	"time"
)

// ErrInvalid 标识无效的请求参数。
var ErrInvalid = errors.New("invalid request")
// ErrConflict 标识重复配置或违反数据库约束。
var ErrConflict = errors.New("conflicting configuration")

// LocalDateTime 实现与已有服务一致的时间序列化格式。
type LocalDateTime time.Time

// MarshalJSON 输出不带时区后缀的日期时间。
func (t LocalDateTime) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Time(t).Format("2006-01-02T15:04:05.999999"))
}

// Node 定义完整配置节点及层级字段。
type Node struct {
	ID            int64         `json:"id"`
	NodeType      string        `json:"nodeType"`
	EnvType       *string       `json:"envType"`
	GameID        *int64        `json:"gameId"`
	RegionID      *int64        `json:"regionId"`
	ServerGroupID *int64        `json:"serverGroupId"`
	Code          string        `json:"code"`
	Name          string        `json:"name"`
	Status        int           `json:"status"`
	Config        string        `json:"config"`
	Remark        *string       `json:"remark"`
	CreatedAt     LocalDateTime `json:"createdAt"`
	UpdatedAt     LocalDateTime `json:"updatedAt"`
	ParentID      int64         `json:"parentId"`
}

// Parent 根据节点类型返回直接父节点 ID，游戏节点返回 0。
func (n Node) Parent() int64 {
	switch n.NodeType {
	case "region":
		return value(n.GameID)
	case "server_group":
		return value(n.RegionID)
	case "channel_app":
		return value(n.ServerGroupID)
	default:
		return 0
	}
}

func value(id *int64) int64 {
	if id == nil {
		return 0
	}
	return *id
}

// NodeSummary 定义轻量配置树节点。
type NodeSummary struct {
	ID       int64  `json:"id"`
	ParentID int64  `json:"parentId"`
	NodeType string `json:"nodeType"`
	EnvType  *string `json:"envType"`
	Code     string `json:"code"`
	Name     string `json:"name"`
	Status   int    `json:"status"`
	Leaf     bool   `json:"leaf"`
}

// Summary 将节点转换为树列表摘要。
func Summary(n Node) NodeSummary {
	return NodeSummary{ID: n.ID, ParentID: n.Parent(), NodeType: n.NodeType, EnvType: n.EnvType, Code: n.Code, Name: n.Name, Status: n.Status, Leaf: n.NodeType == "server_group" || n.NodeType == "channel_app"}
}

// NodePage 定义分页节点结果。
type NodePage struct {
	Items []NodeSummary `json:"items"`
	Total int64         `json:"total"`
	Page  int           `json:"page"`
	Size  int           `json:"size"`
}

// NodeMatch 包含匹配节点与祖先路径。
type NodeMatch struct {
	Node NodeSummary   `json:"node"`
	Path []NodeSummary `json:"path"`
}

// Revision 定义同步实例标识及版本号。
type Revision struct {
	Epoch    string `json:"epoch"`
	Revision int64  `json:"revision"`
}

// ChangeEvent 定义节点增量变更记录。
type ChangeEvent struct {
	Revision  int64         `json:"revision"`
	NodeID    int64         `json:"nodeId"`
	Operation string        `json:"operation"`
	CreatedAt LocalDateTime `json:"createdAt"`
}

// Snapshot 定义同一数据库版本下的完整节点快照。
type Snapshot struct {
	Epoch    string `json:"epoch"`
	Revision int64  `json:"revision"`
	Nodes    []Node `json:"nodes"`
}

// Subscriber 定义回调订阅者及投递状态。
type Subscriber struct {
	ID                   int64   `json:"id"`
	Code                 string  `json:"code"`
	CallbackURL          string  `json:"callbackUrl"`
	Enabled              bool    `json:"enabled"`
	AcknowledgedRevision int64   `json:"acknowledgedRevision"`
	Failures             int     `json:"failures"`
	LastError            *string `json:"lastError"`
}
