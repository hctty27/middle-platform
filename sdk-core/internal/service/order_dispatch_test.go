package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"hitall.com/middle-platform/sdk-core/internal/model"
	"hitall.com/middle-platform/sdk-core/internal/notify"
)

type dispatchRepo struct {
	workerQueueRepo
	mu       sync.Mutex
	pending  []model.Order
	claims   map[int64]int
	results  []isolationResult
	attempts chan int64
	claimErr error
	stale    bool
	staleIDs map[int64]bool
	listed   chan struct{}
}

func (r *dispatchRepo) ListDue(ctx context.Context, after *model.NotifyCandidate, limit int) ([]model.NotifyCandidate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	select {
	case r.listed <- struct{}{}:
	default:
	}
	var items []model.NotifyCandidate
	for _, o := range r.pending {
		if after != nil && o.ID <= after.ID {
			continue
		}
		items = append(items, model.NotifyCandidate{ID: o.ID, GameNotifyURL: o.GameNotifyURL})
		if len(items) == limit {
			break
		}
	}
	return items, nil
}

func (r *dispatchRepo) ClaimCandidate(ctx context.Context, candidate model.NotifyCandidate) (*model.Order, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	select {
	case r.attempts <- candidate.ID:
	default:
	}
	if r.claimErr != nil || r.stale || r.staleIDs[candidate.ID] {
		return nil, r.claimErr
	}
	for i, o := range r.pending {
		if o.ID == candidate.ID {
			if r.claims == nil {
				r.claims = make(map[int64]int)
			}
			r.claims[o.ID]++
			r.pending = append(r.pending[:i], r.pending[i+1:]...)
			o.NotifyAttempts++
			return &o, nil
		}
	}
	return nil, nil
}

func (r *dispatchRepo) CompleteNotify(ctx context.Context, id int64, attempt int, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.results = append(r.results, isolationResult{id, attempt, err})
	return nil
}

func startDispatcher(t *testing.T, svc *OrderService, workers int, idle time.Duration) (context.Context, context.CancelFunc, <-chan struct{}) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	done := make(chan struct{})
	go func() {
		defer close(done)
		svc.RunWorker(ctx, workers, idle)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("dispatcher failed to shut down")
		}
	})
	return ctx, cancel, done
}

func TestDispatcherSkipsBusyTargetsBeforeClaimAndAdvancesPages(t *testing.T) {
	gate := notify.NewHostGate(1)
	release, ok := gate.TryAcquire("https://busy.example/held")
	if !ok {
		t.Fatal("cannot hold busy host quota")
	}
	defer release()
	repo := &dispatchRepo{staleIDs: map[int64]bool{64: true}}
	for i := int64(1); i <= 260; i++ {
		target := "https://BUSY.example:443/order"
		if i == 64 {
			target = "https://locked.example/notify"
		}
		repo.pending = append(repo.pending, model.Order{ID: i, GameID: 1, GameNotifyURL: target})
	}
	repo.pending = append(repo.pending, model.Order{ID: 261, GameID: 2, GameNotifyURL: "https://healthy.example/notify"})
	notifier := workerNotifier{sent: make(chan int64, 300)}
	svc := &OrderService{Orders: repo, HostGate: gate, GameSecretReader: workerGameConfig{}, Notifier: notifier}
	_, cancel, done := startDispatcher(t, svc, 4, time.Millisecond)
	select {
	case id := <-notifier.sent:
		if id != 261 {
			t.Fatalf("busy candidate was delivered: %d", id)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("healthy target behind multiple busy pages was starved")
	}
	repo.mu.Lock()
	for id, count := range repo.claims {
		if id != 261 || count != 1 {
			t.Errorf("busy target changed a lease/token: %d: %d", id, count)
		}
	}
	repo.mu.Unlock()
	release()
	select {
	case id := <-notifier.sent:
		if id == 261 {
			t.Fatal("completed candidate was delivered again")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("released busy target was not revisited after page wrap")
	}
	cancel()
	<-done
	repo.mu.Lock()
	defer repo.mu.Unlock()
	for _, result := range repo.results {
		if errors.Is(result.err, model.ErrNotifyDeferred) || result.attempt != 1 {
			t.Fatalf("local saturation persisted a deferral or inflated attempts: %+v", result)
		}
	}
}

func TestDispatcherRespectsWorkerAndNormalizedHostLimits(t *testing.T) {
	repo := &dispatchRepo{}
	const total = 60
	for i := int64(1); i <= total; i++ {
		host := i % 5
		repo.pending = append(repo.pending, model.Order{ID: i, GameID: host + 1, GameNotifyURL: fmt.Sprintf("https://game%d.example/notify", host)})
	}
	var mu sync.Mutex
	active, maxActive := 0, 0
	hosts := make(map[int64]int)
	sent := make(chan int64, total)
	notifier := &isolationNotifier{send: func(ctx context.Context, o model.Order) error {
		mu.Lock()
		active++
		hosts[o.GameID]++
		if active > maxActive {
			maxActive = active
		}
		if active > 3 || hosts[o.GameID] > 1 {
			t.Errorf("quota exceeded: total=%d host=%d", active, hosts[o.GameID])
		}
		mu.Unlock()
		defer func() {
			mu.Lock()
			active--
			hosts[o.GameID]--
			mu.Unlock()
		}()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Millisecond):
			sent <- o.ID
			return nil
		}
	}}
	svc := &OrderService{Orders: repo, HostGate: notify.NewHostGate(1), GameSecretReader: workerGameConfig{}, Notifier: notifier}
	ctx, cancel, done := startDispatcher(t, svc, 3, time.Millisecond)
	seen := make(map[int64]bool)
	for len(seen) < total {
		select {
		case id := <-sent:
			if seen[id] {
				t.Fatalf("order %d delivered twice", id)
			}
			seen[id] = true
		case <-ctx.Done():
			t.Fatalf("only delivered %d/%d", len(seen), total)
		}
	}
	cancel()
	<-done
	mu.Lock()
	defer mu.Unlock()
	if maxActive < 2 {
		t.Fatal("dispatcher serialized unrelated targets")
	}
	repo.mu.Lock()
	defer repo.mu.Unlock()
	if len(repo.results) != total {
		t.Fatalf("missing completions: %d", len(repo.results))
	}
	for _, result := range repo.results {
		if result.err != nil || result.attempt != 1 {
			t.Fatalf("unexpected completion: %+v", result)
		}
	}
}

func TestDispatcherReleasesQuotaForStaleCandidateAndClaimError(t *testing.T) {
	for _, mode := range []string{"stale", "error"} {
		t.Run(mode, func(t *testing.T) {
			gate := notify.NewHostGate(1)
			repo := &dispatchRepo{pending: []model.Order{{ID: 1, GameNotifyURL: "https://game.example/notify"}}, attempts: make(chan int64, 1)}
			if mode == "stale" {
				repo.stale = true
			} else {
				repo.claimErr = errors.New("database unavailable")
			}
			notifier := &isolationNotifier{}
			svc := &OrderService{Orders: repo, HostGate: gate, GameSecretReader: workerGameConfig{}, Notifier: notifier}
			_, cancel, done := startDispatcher(t, svc, 4, time.Hour)
			select {
			case <-repo.attempts:
			case <-time.After(time.Second):
				t.Fatal("candidate was not checked")
			}
			cancel()
			<-done
			release, ok := gate.TryAcquire("https://game.example/other")
			if !ok {
				t.Fatal("failed claim leaked target quota")
			}
			release()
			if notifier.calls.Load() != 0 || len(repo.results) != 0 {
				t.Fatal("unclaimed candidate was sent or persisted")
			}
		})
	}
}

func TestDispatchersRefreshSharedCandidatesWithoutDuplicateDelivery(t *testing.T) {
	repo := &dispatchRepo{}
	const total = 180
	for id := int64(1); id <= total; id++ {
		repo.pending = append(repo.pending, model.Order{ID: id, GameID: 1, GameNotifyURL: "https://game.example/notify"})
	}
	sent := make(chan int64, total)
	notifier := workerNotifier{sent: sent}
	first := &OrderService{Orders: repo, HostGate: notify.NewHostGate(1), GameSecretReader: workerGameConfig{}, Notifier: notifier}
	second := &OrderService{Orders: repo, HostGate: notify.NewHostGate(1), GameSecretReader: workerGameConfig{}, Notifier: notifier}
	ctx, cancelFirst, firstDone := startDispatcher(t, first, 4, time.Millisecond)
	_, cancelSecond, secondDone := startDispatcher(t, second, 4, time.Millisecond)
	seen := make(map[int64]bool)
	for len(seen) < total {
		select {
		case id := <-sent:
			if seen[id] {
				t.Fatalf("shared candidate %d delivered twice", id)
			}
			seen[id] = true
		case <-ctx.Done():
			t.Fatalf("shared candidate refresh stalled at %d/%d", len(seen), total)
		}
	}
	cancelFirst()
	cancelSecond()
	<-firstDone
	<-secondDone
	repo.mu.Lock()
	defer repo.mu.Unlock()
	if len(repo.results) != total {
		t.Fatalf("missing shared completions: %d", len(repo.results))
	}
	for id, count := range repo.claims {
		if count != 1 {
			t.Errorf("order %d leased %d times", id, count)
		}
	}
}

func TestDispatcherCancellationDrainsActiveNotifications(t *testing.T) {
	gate := notify.NewHostGate(1)
	repo := &dispatchRepo{pending: []model.Order{{ID: 1, GameID: 1, GameNotifyURL: "https://game.example/notify"}}}
	sending := make(chan struct{})
	notifier := &isolationNotifier{send: func(ctx context.Context, _ model.Order) error {
		close(sending)
		<-ctx.Done()
		return ctx.Err()
	}}
	svc := &OrderService{Orders: repo, HostGate: gate, GameSecretReader: workerGameConfig{}, Notifier: notifier}
	_, cancel, done := startDispatcher(t, svc, 1, time.Hour)
	select {
	case <-sending:
	case <-time.After(time.Second):
		t.Fatal("notification did not start")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("dispatcher did not wait for canceled notification completion")
	}
	release, ok := gate.TryAcquire("https://game.example/notify")
	if !ok {
		t.Fatal("cancellation leaked target quota")
	}
	release()
	repo.mu.Lock()
	defer repo.mu.Unlock()
	if len(repo.results) != 1 || !errors.Is(repo.results[0].err, context.Canceled) {
		t.Fatalf("canceled result not persisted with independent context: %+v", repo.results)
	}
}
