package service

import (
	"context"
	"log/slog"
	"math/rand/v2"
	"sync"
	"time"

	"hitall.com/middle-platform/sdk-core/internal/model"
)

type notifyCandidateQueue interface {
	ListDue(context.Context, *model.NotifyCandidate, int) ([]model.NotifyCandidate, error)
	ClaimCandidate(context.Context, model.NotifyCandidate) (*model.Order, error)
}

// 候选不持有数据库租约；容量只在发送前预留，内存和活跃租约均有界。
func (s *OrderService) runNotifyDispatcher(ctx context.Context, queue notifyCandidateQueue, workers int, idleInterval time.Duration) {
	if workers < 1 {
		return
	}
	const batchSize = 128
	finished := make(chan struct{}, workers)
	var wg sync.WaitGroup
	defer wg.Wait()
	active := 0
	var after *model.NotifyCandidate
	var candidates []model.NotifyCandidate
	var lastErrorLog time.Time
	logError := func(err error) {
		if err != nil && ctx.Err() == nil && time.Since(lastErrorLog) >= 30*time.Second {
			slog.Warn("调度游戏发货通知任务失败", "error", err)
			lastErrorLog = time.Now()
		}
	}
	wait := func() {
		timer := time.NewTimer(idleInterval)
		defer timer.Stop()
		select {
		case <-ctx.Done():
		case <-finished:
			active--
		case <-timer.C:
		}
	}
	for ctx.Err() == nil {
		select {
		case <-finished:
			active--
			continue
		default:
		}
		if active == workers {
			select {
			case <-ctx.Done():
			case <-finished:
				active--
			}
			continue
		}
		if len(candidates) == 0 {
			listCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			batch, err := queue.ListDue(listCtx, after, batchSize)
			cancel()
			if err != nil {
				logError(err)
				wait()
				continue
			}
			if len(batch) == 0 {
				after = nil
				wait()
				continue
			}
			last := batch[len(batch)-1]
			after = &last
			// 多实例不能总从同一个候选开始竞争；仅在当前有界页内打散。
			rand.Shuffle(len(batch), func(i, j int) { batch[i], batch[j] = batch[j], batch[i] })
			candidates = batch
		}
		candidate := candidates[0]
		candidates = candidates[1:]
		release := func() {}
		available := true
		if s.HostGate != nil {
			release, available = s.HostGate.TryAcquire(candidate.GameNotifyURL)
		}
		if available {
			claimCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			order, err := queue.ClaimCandidate(claimCtx, candidate)
			cancel()
			if err != nil || order == nil {
				release()
				// 未领到也保留本页剩余候选和页末游标，锁定行不能阻断后续目标。
				if err != nil {
					logError(err)
					wait()
				}
			} else {
				active++
				wg.Add(1)
				go func(o model.Order, release func()) {
					defer wg.Done()
					defer func() { finished <- struct{}{} }()
					if err := s.deliverReserved(ctx, o, release); err != nil && ctx.Err() == nil {
						slog.Warn("游戏发货通知失败", "order_id", o.ID, "attempt", o.NotifyAttempts, "error", err)
					}
				}(*order, release)
			}
		}
		// 一页全是忙目标时也推进游标，避免健康目标永久被队头挡住。
		// 每页最多一次空闲等待；发送完成会提前唤醒，不逐单写延期。
		if len(candidates) == 0 && active < workers {
			wait()
		}
	}
}
