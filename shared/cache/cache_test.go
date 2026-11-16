package cache

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func newTestCache[K comparable, V any](t *testing.T, o Options, clone func(V) V) *Cache[K, V] {
	t.Helper()
	c, err := New[K, V](o, clone)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	return c
}

func await(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("condition did not become true")
		}
		runtime.Gosched()
	}
}

func TestOptions(t *testing.T) {
	for _, o := range []Options{{TTL: -1}, {MaxEntries: -1}, {MaxInflight: -1}, {LoadTimeout: -1}} {
		if _, err := New[string, int](o, nil); err == nil {
			t.Fatalf("accepted %+v", o)
		}
	}
	c := newTestCache[string, int](t, Options{TTL: time.Second}, nil)
	if c.options.MaxEntries != 4096 || c.options.MaxInflight != 8 || c.options.LoadTimeout != 5*time.Second {
		t.Fatalf("defaults: %+v", c.options)
	}
}

func TestTTLAndErrors(t *testing.T) {
	c := newTestCache[string, int](t, Options{TTL: time.Second}, nil)
	now := time.Unix(100, 0)
	c.now = func() time.Time { return now }
	calls := 0
	failure := errors.New("source unavailable")
	load := func(context.Context) (int, error) {
		calls++
		if calls == 2 {
			return 0, failure
		}
		return calls, nil
	}
	get := func(want int, wantErr error) {
		t.Helper()
		v, err := c.Get(context.Background(), "key", load)
		if v != want || !errors.Is(err, wantErr) {
			t.Fatalf("got %d %v, want %d %v", v, err, want, wantErr)
		}
	}
	get(1, nil)
	now = now.Add(999 * time.Millisecond)
	get(1, nil)
	now = now.Add(time.Millisecond)
	get(0, failure) // Exact expiry; never fall back to the stale success.
	get(3, nil)
	get(3, nil)
	if s := c.Stats(); s.Hits != 2 || s.Loads != 3 || s.LoadErrors != 1 || s.Entries != 1 {
		t.Fatalf("stats %+v", s)
	}
}

func TestTTLStartsAfterLoad(t *testing.T) {
	c := newTestCache[string, int](t, Options{TTL: time.Second}, nil)
	now := time.Unix(100, 0)
	c.now = func() time.Time { return now }
	_, err := c.Get(context.Background(), "key", func(context.Context) (int, error) {
		// Only the loader is running here; advance the injected clock while it runs.
		c.mu.Lock()
		now = now.Add(10 * time.Second)
		c.mu.Unlock()
		return 7, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(999 * time.Millisecond)
	v, err := c.Get(context.Background(), "key", func(context.Context) (int, error) { t.Error("expired too soon"); return 0, nil })
	if err != nil || v != 7 {
		t.Fatalf("%d %v", v, err)
	}
}

func TestLRUBounded(t *testing.T) {
	c := newTestCache[string, string](t, Options{TTL: time.Hour, MaxEntries: 2}, nil)
	calls := map[string]int{}
	get := func(key string) {
		t.Helper()
		v, err := c.Get(context.Background(), key, func(context.Context) (string, error) { calls[key]++; return key, nil })
		if err != nil || v != key {
			t.Fatalf("%s %v", v, err)
		}
	}
	get("a")
	get("b")
	get("a")
	get("c")
	if c.Stats().Entries != 2 {
		t.Fatal(c.Stats())
	}
	get("a")
	get("b")
	if calls["a"] != 1 || calls["b"] != 2 || c.Stats().Evictions != 2 {
		t.Fatalf("calls %v stats %+v", calls, c.Stats())
	}
	for i := 0; i < 100; i++ {
		get(fmt.Sprint(i))
	}
	if c.Stats().Entries != 2 {
		t.Fatal(c.Stats())
	}
}

func TestCloneOnLoadAndEveryRead(t *testing.T) {
	c := newTestCache[string, *int](t, Options{TTL: time.Hour}, func(p *int) *int { v := *p; return &v })
	original := 7
	load := func(context.Context) (*int, error) { return &original, nil }
	a, err := c.Get(context.Background(), "key", load)
	if err != nil {
		t.Fatal(err)
	}
	original = 8
	*a = 9
	b, err := c.Get(context.Background(), "key", load)
	if err != nil || *b != 7 || a == b {
		t.Fatalf("value %v err %v", b, err)
	}
	*b = 10
	d, _ := c.Get(context.Background(), "key", load)
	if *d != 7 {
		t.Fatal(*d)
	}
}

func TestCoalescingCancelAndInflightBound(t *testing.T) {
	c := newTestCache[string, *int](t, Options{TTL: time.Hour, MaxInflight: 1}, func(p *int) *int { v := *p; return &v })
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	load := func(ctx context.Context) (*int, error) {
		calls.Add(1)
		close(started)
		<-release
		v := 42
		return &v, nil
	}
	firstCtx, cancel := context.WithCancel(context.Background())
	first := make(chan error, 1)
	go func() { _, err := c.Get(firstCtx, "same", load); first <- err }()
	<-started
	second := make(chan *int, 1)
	secondErr := make(chan error, 1)
	go func() { v, err := c.Get(context.Background(), "same", load); second <- v; secondErr <- err }()
	await(t, func() bool { return c.Stats().Shared == 1 })
	cancel()
	if err := <-first; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	queueCtx, queueCancel := context.WithCancel(context.Background())
	queued := make(chan error, 1)
	go func() {
		_, err := c.Get(queueCtx, "different", func(context.Context) (*int, error) {
			t.Error("queued loader ran before capacity was available")
			return nil, nil
		})
		queued <- err
	}()
	await(t, func() bool { return c.Stats().Waited == 1 })
	queueCancel()
	if err := <-queued; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if c.Stats().Inflight != 1 {
		t.Fatal(c.Stats())
	}
	close(release)
	v, err := <-second, <-secondErr
	if err != nil || *v != 42 || calls.Load() != 1 {
		t.Fatalf("%v %v calls %d", v, err, calls.Load())
	}
	*v = 99
	again, err := c.Get(context.Background(), "same", load)
	if err != nil || *again != 42 {
		t.Fatalf("%v %v", again, err)
	}
	if s := c.Stats(); s.Waited != 1 || s.Inflight != 0 || s.Loads != 1 {
		t.Fatal(s)
	}
}

func TestWaitingCancellationIndependent(t *testing.T) {
	c := newTestCache[string, int](t, Options{TTL: time.Hour}, nil)
	started, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, err := c.Get(context.Background(), "k", func(ctx context.Context) (int, error) { close(started); <-release; return 1, ctx.Err() })
		done <- err
	}()
	<-started
	ctx, cancel := context.WithCancel(context.Background())
	waiting := make(chan error, 1)
	go func() { _, err := c.Get(ctx, "k", nil); waiting <- err }()
	await(t, func() bool { return c.Stats().Shared == 1 })
	cancel()
	if err := <-waiting; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestTimeoutRetainsSlotUntilLoaderReturns(t *testing.T) {
	c := newTestCache[string, int](t, Options{TTL: time.Hour, MaxInflight: 1, LoadTimeout: 15 * time.Millisecond}, nil)
	release := make(chan struct{})
	_, err := c.Get(context.Background(), "slow", func(context.Context) (int, error) { <-release; return 1, nil })
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Millisecond)
	defer cancel()
	_, err = c.Get(ctx, "other", func(context.Context) (int, error) { t.Error("unbounded load"); return 0, nil })
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if s := c.Stats(); s.Waited != 1 || s.Inflight != 1 || s.Loads != 1 {
		t.Fatal(s)
	}
	close(release)
	await(t, func() bool { return c.Stats().Inflight == 0 })
	if s := c.Stats(); s.Entries != 0 || s.LoadErrors != 1 {
		t.Fatal(s)
	}
	v, err := c.Get(context.Background(), "slow", func(context.Context) (int, error) { return 2, nil })
	if v != 2 || err != nil {
		t.Fatalf("%d %v", v, err)
	}
}

func TestCloseCancelsAndPreventsFill(t *testing.T) {
	c := newTestCache[string, int](t, Options{TTL: time.Hour}, nil)
	_, _ = c.Get(context.Background(), "cached", func(context.Context) (int, error) { return 1, nil })
	started, release := make(chan struct{}), make(chan struct{})
	canceled := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, err := c.Get(context.Background(), "loading", func(ctx context.Context) (int, error) {
			close(started)
			<-ctx.Done()
			close(canceled)
			<-release
			return 2, nil
		})
		done <- err
	}()
	<-started
	c.Close()
	c.Close()
	<-canceled
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if s := c.Stats(); s.Entries != 0 || s.Inflight != 1 {
		t.Fatal(s)
	}
	_, err := c.Get(context.Background(), "cached", func(context.Context) (int, error) { t.Error("load after close"); return 3, nil })
	if !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
	close(release)
	await(t, func() bool { return c.Stats().Inflight == 0 })
	if c.Stats().Entries != 0 {
		t.Fatal(c.Stats())
	}
}

func TestDisabledPassthrough(t *testing.T) {
	c := newTestCache[string, *int](t, Options{}, func(*int) *int { t.Error("disabled clone"); return nil })
	value := 1
	type ctxKey struct{}
	ctx := context.WithValue(context.Background(), ctxKey{}, "caller")
	for i := 0; i < 3; i++ {
		got, err := c.Get(ctx, "k", func(got context.Context) (*int, error) {
			if got != ctx {
				t.Error("context replaced")
			}
			return &value, nil
		})
		if err != nil || got != &value {
			t.Fatalf("%v %v", got, err)
		}
	}
	if s := c.Stats(); s != (Stats{}) || c.entries != nil || c.flights != nil || c.changed != nil {
		t.Fatalf("disabled state %+v", s)
	}
	c.Close()
	if _, err := c.Get(ctx, "k", nil); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
}

func TestDisabledDoesNotCoalesceOrLimit(t *testing.T) {
	c := newTestCache[string, int](t, Options{MaxInflight: 1}, nil)
	var wg sync.WaitGroup
	var count atomic.Int32
	release := make(chan struct{})
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := c.Get(context.Background(), "same", func(context.Context) (int, error) { count.Add(1); <-release; return 1, nil })
			if err != nil {
				t.Error(err)
			}
		}()
	}
	await(t, func() bool { return count.Load() == 12 })
	close(release)
	wg.Wait()
	if c.Stats() != (Stats{}) {
		t.Fatal(c.Stats())
	}
}

func TestAlreadyCanceledDoesNotLoad(t *testing.T) {
	for _, ttl := range []time.Duration{0, time.Second} {
		c := newTestCache[string, int](t, Options{TTL: ttl}, nil)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := c.Get(ctx, "k", func(context.Context) (int, error) { t.Error("canceled loader ran"); return 1, nil })
		if !errors.Is(err, context.Canceled) || c.Stats() != (Stats{}) {
			t.Fatalf("%v %+v", err, c.Stats())
		}
	}
}

func TestDefaultInflightBoundAndSharedClones(t *testing.T) {
	c := newTestCache[int, *int](t, Options{TTL: time.Hour}, func(p *int) *int { v := *p; return &v })
	release := make(chan struct{})
	var started atomic.Int32
	load := func(context.Context) (*int, error) { started.Add(1); <-release; v := 7; return &v, nil }
	results := make(chan *int, 41)
	errs := make(chan error, 41)
	for key := 0; key < 8; key++ {
		go func() { v, err := c.Get(context.Background(), key, load); results <- v; errs <- err }()
	}
	await(t, func() bool { return started.Load() == 8 })
	for i := 0; i < 32; i++ {
		go func() { v, err := c.Get(context.Background(), 0, load); results <- v; errs <- err }()
	}
	await(t, func() bool { return c.Stats().Shared == 32 })
	go func() { v, err := c.Get(context.Background(), 9, load); results <- v; errs <- err }()
	await(t, func() bool { return c.Stats().Waited == 1 })
	if s := c.Stats(); s.Inflight != 8 || s.Loads != 8 || started.Load() != 8 {
		t.Fatal(s)
	}
	close(release)
	seen := make(map[*int]bool)
	for i := 0; i < 41; i++ {
		p, err := <-results, <-errs
		if err != nil || p == nil || *p != 7 || seen[p] {
			t.Fatalf("shared result not isolated: %v", err)
		}
		seen[p] = true
	}
	if s := c.Stats(); s.Entries != 9 || s.Inflight != 0 || s.Waited != 1 || s.Loads != 9 {
		t.Fatal(s)
	}
}

func TestCapacityWaitersRecheckSameKey(t *testing.T) {
	c := newTestCache[string, int](t, Options{TTL: time.Hour, MaxInflight: 1}, nil)
	blockerStarted, unblock := make(chan struct{}), make(chan struct{})
	blockerDone := make(chan error, 1)
	go func() {
		_, err := c.Get(context.Background(), "blocker", func(context.Context) (int, error) { close(blockerStarted); <-unblock; return 1, nil })
		blockerDone <- err
	}()
	<-blockerStarted
	targetStarted, finishTarget := make(chan struct{}), make(chan struct{})
	const count = 24
	results := make(chan error, count)
	var calls atomic.Int32
	for i := 0; i < count; i++ {
		go func() {
			v, err := c.Get(context.Background(), "target", func(context.Context) (int, error) { calls.Add(1); close(targetStarted); <-finishTarget; return 7, nil })
			if err == nil && v != 7 {
				err = fmt.Errorf("got %d, want 7", v)
			}
			results <- err
		}()
	}
	await(t, func() bool { return c.Stats().Waited == count })
	if calls.Load() != 0 || c.Stats().Loads != 1 {
		t.Fatal("started target without capacity")
	}
	close(unblock)
	if err := <-blockerDone; err != nil {
		t.Fatal(err)
	}
	<-targetStarted
	await(t, func() bool { return c.Stats().Shared == count-1 })
	close(finishTarget)
	for i := 0; i < count; i++ {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	if s := c.Stats(); s.Loads != 2 || s.Waited != count || s.Misses != count+1 || s.Inflight != 0 {
		t.Fatal(s)
	}
	if calls.Load() != 1 {
		t.Fatal(calls.Load())
	}
}

func TestCloseWakesCapacityWaiters(t *testing.T) {
	c := newTestCache[int, int](t, Options{TTL: time.Hour, MaxInflight: 1}, nil)
	started, release := make(chan struct{}), make(chan struct{})
	first := make(chan error, 1)
	go func() {
		_, err := c.Get(context.Background(), 0, func(context.Context) (int, error) { close(started); <-release; return 1, nil })
		first <- err
	}()
	<-started
	const count = 24
	queued := make(chan error, count)
	var calls atomic.Int32
	for key := 1; key <= count; key++ {
		go func() {
			_, err := c.Get(context.Background(), key, func(context.Context) (int, error) { calls.Add(1); return 2, nil })
			queued <- err
		}()
	}
	await(t, func() bool { return c.Stats().Waited == count })
	c.Close()
	for i := 0; i < count; i++ {
		if err := <-queued; !errors.Is(err, ErrClosed) {
			t.Fatal(err)
		}
	}
	if err := <-first; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if calls.Load() != 0 || c.Stats().Inflight != 1 {
		t.Fatal("shutdown started a queued load or released an active loader")
	}
	close(release)
	await(t, func() bool { return c.Stats().Inflight == 0 })
	if c.Stats().Entries != 0 {
		t.Fatal(c.Stats())
	}
}

func TestCapacityWaitersRemainBoundedAcrossWakeups(t *testing.T) {
	c := newTestCache[int, int](t, Options{TTL: time.Hour, MaxInflight: 1}, nil)
	started, release := make(chan struct{}), make(chan struct{})
	first := make(chan error, 1)
	go func() {
		_, err := c.Get(context.Background(), 0, func(context.Context) (int, error) { close(started); <-release; return 0, nil })
		first <- err
	}()
	<-started
	const count = 32
	results := make(chan error, count)
	var active atomic.Int32
	var exceeded atomic.Bool
	for key := 1; key <= count; key++ {
		go func() {
			_, err := c.Get(context.Background(), key, func(context.Context) (int, error) {
				if active.Add(1) > 1 {
					exceeded.Store(true)
				}
				runtime.Gosched()
				active.Add(-1)
				return key, nil
			})
			results <- err
		}()
	}
	await(t, func() bool { return c.Stats().Waited == count })
	close(release)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < count; i++ {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	if exceeded.Load() {
		t.Fatal("exceeded MaxInflight")
	}
	if s := c.Stats(); s.Waited != count || s.Misses != count+1 || s.Loads != count+1 || s.Entries != count+1 {
		t.Fatal(s)
	}
}

func BenchmarkCache(b *testing.B) {
	for _, mode := range []string{"hot", "cold", "parallel_hot", "parallel_cold", "disabled"} {
		b.Run(mode, func(b *testing.B) {
			ttl := time.Hour
			if mode == "disabled" {
				ttl = 0
			}
			c, _ := New[uint64, uint64](Options{TTL: ttl}, nil)
			defer c.Close()
			ctx := context.Background()
			var loads atomic.Uint64
			load := func(context.Context) (uint64, error) { loads.Add(1); return 1, nil }
			_, _ = c.Get(ctx, 0, load)
			var keys atomic.Uint64
			b.ReportAllocs()
			b.ResetTimer()
			if mode == "parallel_hot" || mode == "parallel_cold" {
				b.RunParallel(func(pb *testing.PB) {
					for pb.Next() {
						key := uint64(0)
						if mode == "parallel_cold" {
							key = keys.Add(1)
						}
						if _, err := c.Get(ctx, key, load); err != nil {
							b.Error(err)
						}
					}
				})
			} else {
				for i := 0; i < b.N; i++ {
					key := uint64(0)
					if mode == "cold" {
						key = uint64(i + 1)
					}
					if _, err := c.Get(ctx, key, load); err != nil {
						b.Fatal(err)
					}
				}
			}
			b.StopTimer()
			b.ReportMetric(float64(loads.Load()-1)/float64(b.N), "loads/op")
		})
	}
}
