// Package syncer 实现 Webhook 触发和定期校对共用的单飞同步任务。
package syncer

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/hctty27/middle-platform/config-center-access/internal/remote"
	"github.com/hctty27/middle-platform/config-center-access/internal/store"
)

// Client 只依赖远程同步协议，便于使用 httptest 测试。
// Client 定义接入端读取权威版本、变更列表和快照的远端能力。
type Client interface {
	Head(context.Context) (remote.Revision, error)
	Changes(context.Context, int64) ([]remote.Event, error)
	Batch(context.Context, []int64) ([]remote.Node, error)
	Full(context.Context) (remote.Snapshot, error)
}

// Synchronizer 通过本地行锁和游标 CAS 协调同库多实例同步。
type Synchronizer struct {
	local *store.Replica
	remote Client
}

// New 组合本地事务副本与远程同步客户端，不在构造时发起网络请求。
func New(local *store.Replica, remote Client) *Synchronizer {
	return &Synchronizer{local: local, remote: remote}
}

// Sync 读取网络数据时不持有数据库行锁，提交时再检查原游标。
func (s *Synchronizer) Sync(ctx context.Context) error {
	// 多实例游标 CAS 冲突后重新读取权威版本，限制重试次数避免无限循环。
	for retries := 0; retries < 8; retries++ {
		state, err := s.local.ReadState(ctx)
		if err != nil { return err }
		head, err := s.remote.Head(ctx)
		if err != nil { return err }
		if head.Epoch == "" || head.Revision < 0 {
			return fmt.Errorf("配置中心版本号不合法")
		}
		if state.Epoch == "" || state.Epoch != head.Epoch || state.Revision > head.Revision {
			applied, err := s.snapshot(ctx, state)
			if err != nil { return err }
			if !applied { continue }
			// 快照下载期间可能产生新 revision，继续追到最新。
			continue
		}
		if state.Revision == head.Revision { return nil }
		events, err := s.remote.Changes(ctx, state.Revision)
		if err != nil { return err }
		if !contiguous(state.Revision, events) {
			applied, err := s.snapshot(ctx, state)
			if err != nil { return err }
			if !applied { continue }
			continue
		}
		ids := make([]int64, 0, len(events))
		seen := make(map[int64]struct{}, len(events))
		for _, event := range events {
			if event.NodeID <= 0 { return fmt.Errorf("变更事件节点 ID 不合法") }
			if _, ok := seen[event.NodeID]; !ok {
				ids = append(ids, event.NodeID)
				seen[event.NodeID] = struct{}{}
			}
		}
		nodes, err := s.remote.Batch(ctx, ids)
		if err != nil { return err }
		latest := make(map[int64]remote.Node, len(nodes))
		for _, n := range nodes {
			if _, requested := seen[n.ID]; !requested { return fmt.Errorf("批量响应包含未请求的节点 %d", n.ID) }
			if _, exists := latest[n.ID]; exists { return fmt.Errorf("批量响应包含重复节点 %d", n.ID) }
			latest[n.ID] = n
		}
		current, err := s.remote.Head(ctx)
		if err != nil { return err }
		if current.Epoch != head.Epoch { continue }
		deleted := make([]int64, 0)
		for _, id := range ids {
			if _, ok := latest[id]; !ok { deleted = append(deleted, id) }
		}
		applied, err := s.local.Apply(ctx, state, store.State{
			Epoch: head.Epoch, Revision: events[len(events)-1].Revision,
		}, nodes, deleted, false)
		if err != nil { return err }
		if !applied { continue }
		// 可能还有多页变更；继续直到追平 head。
		retries = -1
	}
	return fmt.Errorf("配置同步超过重试次数限制")
}

// snapshot 只从权威端的一致性接口构建完整副本。
func (s *Synchronizer) snapshot(ctx context.Context, expected store.State) (bool, error) {
	data, err := s.remote.Full(ctx)
	if err != nil { return false, err }
	if data.Epoch == "" || data.Revision < 0 || data.Nodes == nil {
		return false, fmt.Errorf("配置快照不合法")
	}
	seen := make(map[int64]struct{}, len(data.Nodes))
	for _, node := range data.Nodes {
		if _, exists := seen[node.ID]; exists {
			return false, fmt.Errorf("配置快照存在重复节点 %d", node.ID)
		}
		seen[node.ID] = struct{}{}
	}
	return s.local.Apply(ctx, expected, store.State{
		Epoch: data.Epoch, Revision: data.Revision,
	}, data.Nodes, nil, true)
}

// contiguous 验证事件版本连续，缺失事件时必须重新拉取完整快照。
func contiguous(after int64, events []remote.Event) bool {
	if len(events) == 0 || len(events) > 200 { return false }
	for _, event := range events {
		after++
		if event.Revision != after { return false }
	}
	return true
}

// Trigger 合并高频回调；执行失败由下一次通知或周期校对补偿。
// Trigger 将多次回调合并为同步信号，并保留周期性校对能力。
type Trigger struct {
	sync *Synchronizer
	period time.Duration
	pending chan struct{}
}

// NewTrigger 用容量有限的通知通道合并高频回调请求。
func NewTrigger(s *Synchronizer, period time.Duration) *Trigger {
	return &Trigger{sync: s, period: period, pending: make(chan struct{}, 1)}
}

// Request 非阻塞地提交一次后台同步，重复请求会合并。
func (t *Trigger) Request() {
	select {
	case t.pending <- struct{}{}:
	default:
	}
}

// Run 在启动 3 秒后首次校对，以后每隔 period 校对一次。
// Run 等待通知或周期计时触发同步，直到进程请求退出。
func (t *Trigger) Run(ctx context.Context) {
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	ticker := time.NewTicker(t.period)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		case <-ticker.C:
		case <-t.pending:
		}
		if err := t.sync.Sync(ctx); err != nil && ctx.Err() == nil {
			slog.Warn("配置同步失败，将在下次通知或定期对账时重试", "error", err)
		}
	}
}
