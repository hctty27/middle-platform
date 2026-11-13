package service

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"hitall.com/middle-platform/sdk-core/internal/model"
)

type workerQueueRepo struct {
	mu      sync.Mutex
	orders  []model.Order
	claimed chan struct{}
}

func (r *workerQueueRepo) Find(context.Context, string, string) (model.Order, error) {
	return model.Order{}, errors.New("unexpected Find")
}

func (r *workerQueueRepo) Register(context.Context, model.Order) error {
	return errors.New("unexpected Register")
}

func (r *workerQueueRepo) ConfirmPayment(context.Context, int64, string, time.Time) error {
	return errors.New("unexpected ConfirmPayment")
}

func (r *workerQueueRepo) CompleteNotify(context.Context, int64, int, error) error {
	return nil
}

func (r *workerQueueRepo) ClaimDue(ctx context.Context) (*model.Order, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	select {
	case r.claimed <- struct{}{}:
	default:
	}
	if len(r.orders) == 0 {
		return nil, nil
	}
	order := r.orders[0]
	r.orders = r.orders[1:]
	return &order, nil
}

type workerGameConfig struct{}

func (workerGameConfig) LookupGameConfig(context.Context, int64) (json.RawMessage, error) {
	return json.RawMessage(`{"gameSecret":"test-game-secret"}`), nil
}

type workerNotifier struct {
	sent chan int64
}

func (n workerNotifier) ValidateTarget(string) error { return nil }

func (n workerNotifier) Send(ctx context.Context, order model.Order, _ []byte) error {
	select {
	case n.sent <- order.ID:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestRunWorkerDrainsBacklogWithoutIdleDelay(t *testing.T) {
	orders := &workerQueueRepo{
		orders: []model.Order{{ID: 1, GameID: 10}, {ID: 2, GameID: 10}, {ID: 3, GameID: 10}},
		claimed: make(chan struct{}, 8),
	}
	notifier := workerNotifier{sent: make(chan int64, 3)}
	svc := &OrderService{Orders: orders, GameSecretReader: workerGameConfig{}, Notifier: notifier}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		svc.RunWorker(ctx, 1, time.Hour)
	}()
	defer cancel()

	for expected := int64(1); expected <= 3; expected++ {
		select {
		case actual := <-notifier.sent:
			if actual != expected {
				t.Fatalf("expected order %d, got %d", expected, actual)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("worker did not drain backlog without idle delay")
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not stop after cancellation")
	}
}

func TestRunWorkerSleepsWhenIdleAndExitsPromptly(t *testing.T) {
	orders := &workerQueueRepo{claimed: make(chan struct{}, 8)}
	svc := &OrderService{Orders: orders}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		svc.RunWorker(ctx, 1, time.Hour)
	}()
	defer cancel()

	select {
	case <-orders.claimed:
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not check for due orders immediately")
	}
	select {
	case <-orders.claimed:
		t.Fatal("worker polled again while idle")
	case <-time.After(50 * time.Millisecond):
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("idle worker did not exit promptly")
	}
}

func TestRunWorkerStartsConfiguredConsumers(t *testing.T) {
	orders := &workerQueueRepo{claimed: make(chan struct{}, 8)}
	svc := &OrderService{Orders: orders}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		svc.RunWorker(ctx, 3, time.Hour)
	}()
	defer cancel()

	for i := 0; i < 3; i++ {
		select {
		case <-orders.claimed:
		case <-time.After(2 * time.Second):
			t.Fatal("configured workers did not start")
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("workers did not exit after cancellation")
	}
}
