// Package model 定义节点、订阅者以及配置同步数据模型。
package model

import (
	"encoding/json"
	"errors"
	"time"
)

// ErrInvalid 标识无效的请求参数。
var ErrInvalid = errors.New("请求参数不合法")

// ErrConflict 标识业务数据冲突。
var ErrConflict = errors.New("数据冲突")

// ErrSubscriberNotFound 标识同步订阅不存在。
var ErrSubscriberNotFound = errors.New("订阅不存在")

var (
	ErrDuplicateCode         = errors.New("编码已存在，请更换编码")
	ErrDuplicateSecret       = errors.New("签名密钥已被其他接入端使用，请更换密钥")
	ErrDuplicateValue        = errors.New("唯一字段已存在，请检查输入内容")
	ErrGameHasRegions        = errors.New("该游戏下还有大区，请先删除所属大区")
	ErrGameHasChannelApps    = errors.New("该游戏下还有渠道应用，请先删除渠道应用")
	ErrRegionHasServerGroups = errors.New("该大区下还有区服组，请先删除所属区服组")
	ErrServerGroupInUse      = errors.New("该区服组仍被渠道应用绑定，请先解除绑定")
)

type LocalDateTime time.Time

func (t LocalDateTime) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Time(t).UTC().Truncate(time.Second).Format(time.RFC3339))
}

// Node 定义完整配置节点及层级字段。
type Node struct {
	ID       int64  `json:"id"`
	NodeType string `json:"nodeType"`
	// EnvType 仅大区节点使用：test 测试、review 提审、prod 正式。
	EnvType *string `json:"envType"`
	// MarketType 仅游戏节点使用：domestic 国内、overseas 海外。
	MarketType *string       `json:"marketType"`
	GameID     *int64        `json:"gameId"`
	RegionID   *int64        `json:"regionId"`
	Code       string        `json:"code"`
	Name       string        `json:"name"`
	Status     int           `json:"status"`
	Config     string        `json:"config"`
	Remark     *string       `json:"remark"`
	CreatedAt  LocalDateTime `json:"createdAt"`
	UpdatedAt  LocalDateTime `json:"updatedAt"`
	// ParentID 是基于祖先 ID 推导出的接口字段，不单独作为数据库关系列维护。
	ParentID int64 `json:"parentId"`
}

// Parent 根据节点类型返回直接父节点 ID，游戏节点返回 0。
func (n Node) Parent() int64 {
	switch n.NodeType {
	case "region":
		return value(n.GameID)
	case "server_group":
		return value(n.RegionID)
	default:
		return 0
	}
}

// value 将可空祖先 ID 转换为接口所需的整数，空值表示根层级。
func value(id *int64) int64 {
	if id == nil {
		return 0
	}
	return *id
}

// NodeSummary 定义轻量配置树节点。
type NodeSummary struct {
	ID         int64   `json:"id"`
	ParentID   int64   `json:"parentId"`
	NodeType   string  `json:"nodeType"`
	EnvType    *string `json:"envType"`
	MarketType *string `json:"marketType"`
	Code       string  `json:"code"`
	Name       string  `json:"name"`
	Status     int     `json:"status"`
	// Leaf 服务于管理端懒加载目录，不代表配置数据库中一定不存在更低层级。
	Leaf bool `json:"leaf"`
}

// Summary 将节点转换为树列表摘要。
func Summary(n Node) NodeSummary {
	return NodeSummary{ID: n.ID, ParentID: n.Parent(), NodeType: n.NodeType, EnvType: n.EnvType, MarketType: n.MarketType, Code: n.Code, Name: n.Name, Status: n.Status, Leaf: n.NodeType == "server_group"}
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
