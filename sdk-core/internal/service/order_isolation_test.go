package service

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"hitall.com/middle-platform/sdk-core/internal/model"
	"hitall.com/middle-platform/sdk-core/internal/notify"
)

type isolationReader struct {
	calls atomic.Int32
	err   error
}

func (r *isolationReader) LookupGameConfig(context.Context, int64) (json.RawMessage, error) {
	r.calls.Add(1)
	return json.RawMessage(`{"gameSecret":"test-game-secret"}`), r.err
}

type isolationNotifier struct {
	calls atomic.Int32
	send  func(context.Context, model.Order) error
}

func (n *isolationNotifier) ValidateTarget(string) error { return nil }
func (n *isolationNotifier) Send(ctx context.Context, o model.Order, _ []byte) error {
	n.calls.Add(1)
	if n.send != nil {
		return n.send(ctx, o)
	}
	return nil
}

type isolationResult struct {
	id      int64
	attempt int
	err     error
}
type isolationRepo struct {
	workerQueueRepo
	mu      sync.Mutex
	results []isolationResult
	save    func(context.Context, int64) error
}

func (r *isolationRepo) CompleteNotify(ctx context.Context, id int64, attempt int, err error) error {
	r.mu.Lock()
	r.results = append(r.results, isolationResult{id, attempt, err})
	r.mu.Unlock()
	if r.save != nil {
		return r.save(ctx, id)
	}
	return nil
}

func TestDeliverHostIsolationFastRejectAndReleaseBeforeSave(t *testing.T) {
	gate := notify.NewHostGate(1)
	reader := &isolationReader{}
	sending := make(chan struct{})
	finishSend := make(chan struct{})
	saving := make(chan struct{})
	finishSave := make(chan struct{})
	notifier := &isolationNotifier{send: func(_ context.Context, o model.Order) error {
		if o.ID == 1 {
			close(sending)
			<-finishSend
		}
		return nil
	}}
	repo := &isolationRepo{save: func(_ context.Context, id int64) error {
		if id == 1 {
			close(saving)
			<-finishSave
		}
		return nil
	}}
	svc := &OrderService{HostGate: gate, GameSecretReader: reader, Notifier: notifier, Orders: repo}
	order := model.Order{ID: 1, GameID: 10, NotifyAttempts: 7, GameNotifyURL: "http://GAME.example/first"}
	done := make(chan error, 1)
	go func() { done <- svc.deliver(context.Background(), order) }()
	<-sending
	// App and path differ but the effective host is the same.
	same := order
	same.ID = 2
	same.AppID = "other"
	same.GameNotifyURL = "http://game.example:80/second"
	rejected := make(chan error, 1)
	go func() { rejected <- svc.deliver(context.Background(), same) }()
	select {
	case err := <-rejected:
		if !errors.Is(err, model.ErrNotifyDeferred) {
			t.Fatalf("expected deferral: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("saturated host waited")
	}
	if reader.calls.Load() != 1 || notifier.calls.Load() != 1 {
		t.Fatal("rejected work read a key or sent")
	}
	other := order
	other.ID = 3
	other.GameNotifyURL = "http://other.example/path"
	if err := svc.deliver(context.Background(), other); err != nil {
		t.Fatal(err)
	}
	close(finishSend)
	<-saving
	// Saving order 1 must not hold the host quota.
	same.ID = 4
	if err := svc.deliver(context.Background(), same); err != nil {
		t.Fatalf("quota retained during persistence: %v", err)
	}
	close(finishSave)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	repo.mu.Lock()
	defer repo.mu.Unlock()
	found := false
	for _, r := range repo.results {
		if r.id == 2 {
			found = true
			if r.attempt != 7 || !errors.Is(r.err, model.ErrNotifyDeferred) {
				t.Fatalf("wrong completion: %+v", r)
			}
		}
	}
	if !found {
		t.Fatal("deferred task not persisted")
	}
}

func TestDeliverReleasesOnErrorsAndDoesNotResendAfterSaveFailure(t *testing.T) {
	for _, mode := range []string{"key", "network", "cancel", "save", "nil-gate"} {
		t.Run(mode, func(t *testing.T) {
			gate := notify.NewHostGate(1)
			reader := &isolationReader{}
			notifier := &isolationNotifier{}
			repo := &isolationRepo{}
			svc := &OrderService{HostGate: gate, GameSecretReader: reader, Notifier: notifier, Orders: repo}
			ctx := context.Background()
			failure := errors.New("failure")
			switch mode {
			case "key":
				reader.err = failure
			case "network":
				notifier.send = func(context.Context, model.Order) error { return failure }
			case "cancel":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
				notifier.send = func(ctx context.Context, _ model.Order) error { return ctx.Err() }
			case "save":
				repo.save = func(ctx context.Context, _ int64) error {
					if ctx.Err() != nil {
						t.Error("completion context canceled")
					}
					return failure
				}
			case "nil-gate":
				svc.HostGate = nil
			}
			o := model.Order{ID: 1, GameID: 10, NotifyAttempts: 3, GameNotifyURL: "http://game.example/path"}
			err := svc.deliver(ctx, o)
			if mode == "nil-gate" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil {
				t.Fatal("expected failure")
			}
			release, ok := gate.TryAcquire(o.GameNotifyURL)
			if !ok {
				t.Fatal("error leaked host quota")
			}
			release()
			if len(repo.results) != 1 || repo.results[0].attempt != 3 {
				t.Fatal("completion missing or repeated")
			}
			want := int32(1)
			if mode == "key" {
				want = 0
			}
			if notifier.calls.Load() != want {
				t.Fatal("notification was retried locally")
			}
		})
	}
}
