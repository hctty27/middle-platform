// Package store 负责 MySQL 读写、节点查询和数据库事务。
package store

import (
	"github.com/hctty27/middle-platform/config-center/internal/model"
)

// 统一使用 model 中的数据结构，避免 HTTP 层与数据库层出现同义结构体。
type Node = model.Node
type NodeSummary = model.NodeSummary
type NodePage = model.NodePage
type NodeMatch = model.NodeMatch
type Revision = model.Revision
type ChangeEvent = model.ChangeEvent
type Snapshot = model.Snapshot
type Subscriber = model.Subscriber
type LocalDateTime = model.LocalDateTime
type CreateNodeRequest = model.CreateNodeRequest
type UpdateNodeRequest = model.UpdateNodeRequest
type CreateSubscriberRequest = model.CreateSubscriberRequest
var ErrInvalid = model.ErrInvalid
var ErrConflict = model.ErrConflict
// summary 统一生成管理端列表摘要，避免在各 SQL 查询中重复构造结构。
func summary(n Node) NodeSummary { return model.Summary(n) }
// value 将可空祖先 ID 转换为树节点可使用的整数值。
func value(id *int64) int64 {
    if id == nil { return 0 }
    return *id
}
