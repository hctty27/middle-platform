package service

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"hitall.com/middle-platform/sdk-core/internal/model"
	"hitall.com/middle-platform/shared/cache"
)

type orderCacheFake struct {
	lookup   func(context.Context, string, string) (model.ChannelApp, *model.BoundGroup, error)
	platform func(context.Context, string) (string, error)
	game     func(context.Context, int64) (json.RawMessage, error)
	calls    [3]atomic.Int64
}

func (f *orderCacheFake) Lookup(ctx context.Context, app, env string) (model.ChannelApp, *model.BoundGroup, error) {
	f.calls[0].Add(1)
	if f.lookup != nil {
		return f.lookup(ctx, app, env)
	}
	return model.ChannelApp{Code: app}, &model.BoundGroup{RegionCode: env}, nil
}

func (f *orderCacheFake) LookupPlatformSecret(ctx context.Context, app string) (string, error) {
	f.calls[1].Add(1)
	if f.platform != nil {
		return f.platform(ctx, app)
	}
	return "platform-" + app, nil
}

func (f *orderCacheFake) LookupGameConfig(ctx context.Context, game int64) (json.RawMessage, error) {
	f.calls[2].Add(1)
	if f.game != nil {
		return f.game(ctx, game)
	}
	return json.RawMessage(`{"gameSecret":"retained"}`), nil
}

// Each operation uses an intentionally overlapping logical ID.
var orderCacheReads = []struct {
	name string
	read func(context.Context, *CachedOrderReader) error
}{
	{"lookup", func(ctx context.Context, r *CachedOrderReader) error {
		_, _, err := r.Lookup(ctx, "1", "prod")
		return err
	}},
	{"platform", func(ctx context.Context, r *CachedOrderReader) error {
		_, err := r.LookupPlatformSecret(ctx, "1")
		return err
	}},
	{"game", func(ctx context.Context, r *CachedOrderReader) error {
		_, err := r.LookupGameConfig(ctx, 1)
		return err
	}},
}

func newOrderCacheTestReader(t *testing.T, f *orderCacheFake, options cache.Options) *CachedOrderReader {
	t.Helper()
	r, err := NewCachedOrderReader(f, options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Close)
	return r
}

func waitOrderCache(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for order cache condition")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestCachedOrderReaderKeysAndCopies(t *testing.T) {
	for _, ttl := range []time.Duration{0, time.Second} {
		t.Run(ttl.String(), func(t *testing.T) {
			app := model.ChannelApp{ID: 7, GameID: 9, Code: "1", Name: "app", GameCode: "game",
				Config: json.RawMessage(`{"appSecret":"platform"}`), GameConfig: json.RawMessage(`{"gameSecret":"game"}`), GameSDKConfig: json.RawMessage(`{"gameSecret":"sdk"}`)}
			group := &model.BoundGroup{RegionCode: "region", ServerGroupCode: "group",
				RegionConfig: json.RawMessage(`{"a":1}`), RegionSDKConfig: json.RawMessage(`{"payNotifyUrl":"https://game.example/pay"}`),
				Config: json.RawMessage(`{"b":2}`), SDKConfig: json.RawMessage(`{"c":3}`)}
			game := json.RawMessage(`{"gameSecret":"current"}`)
			original := cloneOrderConfig(orderConfigSnapshot{app: app, group: group, game: game})
			f := &orderCacheFake{
				lookup: func(context.Context, string, string) (model.ChannelApp, *model.BoundGroup, error) {
					return app, group, nil
				},
				game: func(context.Context, int64) (json.RawMessage, error) { return game, nil },
			}
			r := newOrderCacheTestReader(t, f, cache.Options{TTL: ttl})
			ctx := context.Background()
			a, g, err := r.Lookup(ctx, "1", "prod")
			if err != nil || !reflect.DeepEqual(a, original.app) || !reflect.DeepEqual(g, original.group) {
				t.Fatal("order configuration or sensitive fields changed")
			}
			v, err := r.LookupGameConfig(ctx, 1)
			if err != nil || !reflect.DeepEqual(v, original.game) {
				t.Fatal("game configuration changed")
			}
			a.Code, g.RegionCode = "caller", "caller"
			for _, raw := range []json.RawMessage{a.Config, a.GameConfig, a.GameSDKConfig, g.RegionConfig, g.RegionSDKConfig, g.Config, g.SDKConfig, v} {
				raw[0] = '!'
			}
			if !reflect.DeepEqual(app, original.app) || !reflect.DeepEqual(group, original.group) || !reflect.DeepEqual(game, original.game) {
				t.Fatal("caller mutated repository values")
			}
			if ttl != 0 {
				app.Code, group.RegionCode = "source", "source"
				for _, raw := range []json.RawMessage{app.Config, app.GameConfig, app.GameSDKConfig, group.RegionConfig, group.RegionSDKConfig, group.Config, group.SDKConfig, game} {
					raw[0] = '?'
				}
			}
			a, g, err = r.Lookup(ctx, "1", "prod")
			if err != nil || !reflect.DeepEqual(a, original.app) || !reflect.DeepEqual(g, original.group) {
				t.Fatal("cached order snapshot mutated")
			}
			v, err = r.LookupGameConfig(ctx, 1)
			if err != nil || !reflect.DeepEqual(v, original.game) {
				t.Fatal("cached game snapshot mutated")
			}
			secret, err := r.LookupPlatformSecret(ctx, "1")
			if err != nil || secret != "platform-1" {
				t.Fatal("platform secret missing or key collided")
			}
			_, _, _ = r.Lookup(ctx, "1", "test")
			_, _, _ = r.Lookup(ctx, "other", "prod")
			_, _, _ = r.Lookup(ctx, "a/b", "c")
			_, _, _ = r.Lookup(ctx, "a", "b/c")
			_, _ = r.LookupPlatformSecret(ctx, "other")
			_, _ = r.LookupGameConfig(ctx, 2)
			wantLookup, wantGame := int64(5), int64(2)
			if ttl == 0 {
				wantLookup++
				wantGame++
			}
			if f.calls[0].Load() != wantLookup || f.calls[1].Load() != 2 || f.calls[2].Load() != wantGame {
				t.Fatal("keys not isolated or disabled reader cached")
			}
		})
	}
}

func TestCachedOrderReaderNilAndEmpty(t *testing.T) {
	f := &orderCacheFake{
		lookup: func(context.Context, string, string) (model.ChannelApp, *model.BoundGroup, error) {
			return model.ChannelApp{Config: json.RawMessage{}}, nil, nil
		},
		game:     func(context.Context, int64) (json.RawMessage, error) { return nil, nil },
		platform: func(context.Context, string) (string, error) { return "", nil },
	}
	r := newOrderCacheTestReader(t, f, cache.Options{TTL: time.Second})
	for i := 0; i < 2; i++ {
		a, g, err := r.Lookup(context.Background(), "1", "prod")
		if err != nil || g != nil || a.Config == nil || len(a.Config) != 0 || a.GameConfig != nil {
			t.Fatal("nil/empty structure semantics changed")
		}
		game, err := r.LookupGameConfig(context.Background(), 1)
		if err != nil || game != nil {
			t.Fatal("nil game semantics changed")
		}
		secret, err := r.LookupPlatformSecret(context.Background(), "1")
		if err != nil || secret != "" {
			t.Fatal("empty secret semantics changed")
		}
	}
	for i := range f.calls {
		if f.calls[i].Load() != 1 {
			t.Fatal("successful empty value not cached")
		}
	}
}

func TestCachedOrderReaderTTLAndErrors(t *testing.T) {
	for index, op := range orderCacheReads {
		t.Run(op.name, func(t *testing.T) {
			var failure atomic.Bool
			failure.Store(true)
			errRead := errors.New("repository unavailable")
			resultErr := func() error {
				if failure.Load() {
					return errRead
				}
				return nil
			}
			f := &orderCacheFake{
				lookup: func(context.Context, string, string) (model.ChannelApp, *model.BoundGroup, error) {
					return model.ChannelApp{}, nil, resultErr()
				},
				platform: func(context.Context, string) (string, error) { return "secret", resultErr() },
				game:     func(context.Context, int64) (json.RawMessage, error) { return json.RawMessage(`{}`), resultErr() },
			}
			r := newOrderCacheTestReader(t, f, cache.Options{TTL: 30 * time.Millisecond})
			ctx := context.Background()
			for i := 0; i < 2; i++ {
				if err := op.read(ctx, r); !errors.Is(err, errRead) {
					t.Fatal(err)
				}
			}
			if f.calls[index].Load() != 2 || r.Stats().Entries != 0 {
				t.Fatal("error cached")
			}
			failure.Store(false)
			for i := 0; i < 2; i++ {
				if err := op.read(ctx, r); err != nil {
					t.Fatal(err)
				}
			}
			if f.calls[index].Load() != 3 {
				t.Fatal("successful result not cached")
			}
			time.Sleep(60 * time.Millisecond)
			if err := op.read(ctx, r); err != nil {
				t.Fatal(err)
			}
			if f.calls[index].Load() != 4 {
				t.Fatal("expired result reused")
			}
			r.Close()
			if err := op.read(ctx, r); !errors.Is(err, cache.ErrClosed) {
				t.Fatal(err)
			}
			if r.Stats().Entries != 0 {
				t.Fatal("Close retained entries")
			}
		})
	}
}

func TestCachedOrderReaderRotationAndDisablement(t *testing.T) {
	var rotated, disabled atomic.Bool
	secret := func() string {
		if rotated.Load() {
			return "new"
		}
		return "old"
	}
	f := &orderCacheFake{
		lookup: func(context.Context, string, string) (model.ChannelApp, *model.BoundGroup, error) {
			if disabled.Load() {
				return model.ChannelApp{}, nil, model.ErrNotFound
			}
			return model.ChannelApp{GameSDKConfig: json.RawMessage(secret())}, &model.BoundGroup{}, nil
		},
		platform: func(context.Context, string) (string, error) { return secret(), nil },
		game:     func(context.Context, int64) (json.RawMessage, error) { return json.RawMessage(secret()), nil },
	}
	r := newOrderCacheTestReader(t, f, cache.Options{TTL: 30 * time.Millisecond})
	ctx := context.Background()
	check := func(want string) {
		t.Helper()
		app, _, err := r.Lookup(ctx, "1", "prod")
		if err != nil || string(app.GameSDKConfig) != want {
			t.Fatal("registration secret rotation mismatch")
		}
		platform, err := r.LookupPlatformSecret(ctx, "1")
		if err != nil || platform != want {
			t.Fatal("platform secret rotation mismatch")
		}
		game, err := r.LookupGameConfig(ctx, 1)
		if err != nil || string(game) != want {
			t.Fatal("game secret rotation mismatch")
		}
	}
	check("old")
	rotated.Store(true)
	check("old") // No immediate push invalidation on rotation.
	time.Sleep(60 * time.Millisecond)
	check("new")
	disabled.Store(true)
	if _, _, err := r.Lookup(ctx, "1", "prod"); err != nil {
		t.Fatal("cached binding vanished before TTL")
	}
	time.Sleep(60 * time.Millisecond)
	for i := 0; i < 2; i++ {
		if _, _, err := r.Lookup(ctx, "1", "prod"); !errors.Is(err, model.ErrNotFound) {
			t.Fatal(err)
		}
	}
	if f.calls[0].Load() != 4 {
		t.Fatal("disabled binding error was cached")
	}
}

func TestCachedOrderReaderConcurrentMiss(t *testing.T) {
	for index, op := range orderCacheReads {
		t.Run(op.name, func(t *testing.T) {
			started, release := make(chan struct{}), make(chan struct{})
			block := func(ctx context.Context) error {
				close(started)
				select {
				case <-release:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			f := &orderCacheFake{
				lookup: func(ctx context.Context, _, _ string) (model.ChannelApp, *model.BoundGroup, error) {
					return model.ChannelApp{}, nil, block(ctx)
				},
				platform: func(ctx context.Context, _ string) (string, error) { return "secret", block(ctx) },
				game:     func(ctx context.Context, _ int64) (json.RawMessage, error) { return json.RawMessage(`{}`), block(ctx) },
			}
			r := newOrderCacheTestReader(t, f, cache.Options{TTL: time.Second})
			const readers = 20
			results := make(chan error, readers)
			for i := 0; i < readers; i++ {
				go func() { results <- op.read(context.Background(), r) }()
			}
			<-started
			waitOrderCache(t, func() bool { return r.Stats().Shared == readers-1 })
			close(release)
			for i := 0; i < readers; i++ {
				if err := <-results; err != nil {
					t.Fatal(err)
				}
			}
			if f.calls[index].Load() != 1 {
				t.Fatal("concurrent miss not coalesced")
			}
		})
	}
}

func TestCachedOrderReaderDisabledConcurrentAndContext(t *testing.T) {
	for index, op := range orderCacheReads {
		t.Run(op.name, func(t *testing.T) {
			started := make(chan context.Context, 2)
			block := func(ctx context.Context) error { started <- ctx; <-ctx.Done(); return ctx.Err() }
			f := &orderCacheFake{
				lookup: func(ctx context.Context, _, _ string) (model.ChannelApp, *model.BoundGroup, error) {
					return model.ChannelApp{}, nil, block(ctx)
				},
				platform: func(ctx context.Context, _ string) (string, error) { return "", block(ctx) },
				game:     func(ctx context.Context, _ int64) (json.RawMessage, error) { return nil, block(ctx) },
			}
			r := newOrderCacheTestReader(t, f, cache.Options{TTL: 0, MaxInflight: 1})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			results := make(chan error, 2)
			for i := 0; i < 2; i++ {
				go func() { results <- op.read(ctx, r) }()
			}
			for i := 0; i < 2; i++ {
				select {
				case got := <-started:
					if got != ctx {
						t.Fatal("disabled read replaced caller context")
					}
				case <-time.After(3 * time.Second):
					t.Fatal("disabled reads were shared or limited")
				}
			}
			cancel()
			for i := 0; i < 2; i++ {
				if err := <-results; !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
			}
			if f.calls[index].Load() != 2 || r.Stats() != (cache.Stats{}) {
				t.Fatal("disabled cache collected values or stats")
			}
		})
	}
}

func TestCachedOrderReaderSharedCancellationAndBounds(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	f := &orderCacheFake{platform: func(ctx context.Context, _ string) (string, error) {
		close(started)
		select {
		case <-release:
			return "retained-secret", nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}}
	r := newOrderCacheTestReader(t, f, cache.Options{TTL: time.Second, MaxEntries: 1, MaxInflight: 1})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	leader := make(chan error, 1)
	go func() { _, err := r.LookupPlatformSecret(ctx, "1"); leader <- err }()
	<-started
	follower := make(chan error, 1)
	go func() { _, err := r.LookupPlatformSecret(context.Background(), "1"); follower <- err }()
	waitOrderCache(t, func() bool { return r.Stats().Shared == 1 })
	cancel()
	if err := <-leader; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	queuedCtx, cancelQueue := context.WithCancel(context.Background())
	queued := make(chan error, 1)
	go func() { _, err := r.LookupGameConfig(queuedCtx, 1); queued <- err }()
	waitOrderCache(t, func() bool { return r.Stats().Waited == 1 })
	cancelQueue()
	if err := <-queued; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if f.calls[2].Load() != 0 || r.Stats().Inflight != 1 {
		t.Fatal("shared load bound bypassed")
	}
	close(release)
	if err := <-follower; err != nil {
		t.Fatal("leader cancellation canceled follower", err)
	}
	if _, err := r.LookupGameConfig(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if r.Stats().Entries != 1 || r.Stats().Evictions != 1 {
		t.Fatal("entry bound not shared across methods")
	}
	if _, _, err := r.Lookup(context.Background(), "1", "prod"); err != nil {
		t.Fatal(err)
	}
	if r.Stats().Entries != 1 || r.Stats().Evictions != 2 {
		t.Fatal("lookup did not share entry bound")
	}
}

func TestCachedOrderReaderTimeoutCloseAndInvalidOptions(t *testing.T) {
	for _, closeCache := range []bool{false, true} {
		t.Run(map[bool]string{true: "close", false: "timeout"}[closeCache], func(t *testing.T) {
			started := make(chan struct{}, 2)
			f := &orderCacheFake{game: func(ctx context.Context, _ int64) (json.RawMessage, error) {
				started <- struct{}{}
				<-ctx.Done()
				return nil, ctx.Err()
			}}
			timeout := 30 * time.Millisecond
			if closeCache {
				timeout = time.Second
			}
			r := newOrderCacheTestReader(t, f, cache.Options{TTL: time.Second, LoadTimeout: timeout})
			result := make(chan error, 1)
			go func() { _, err := r.LookupGameConfig(context.Background(), 1); result <- err }()
			<-started
			want := context.DeadlineExceeded
			if closeCache {
				r.Close()
				want = context.Canceled
			}
			if err := <-result; !errors.Is(err, want) {
				t.Fatal(err)
			}
			waitOrderCache(t, func() bool { return r.Stats().Inflight == 0 })
			if r.Stats().Entries != 0 {
				t.Fatal("canceled load cached")
			}
			if !closeCache {
				_, err := r.LookupGameConfig(context.Background(), 1)
				if !errors.Is(err, context.DeadlineExceeded) || f.calls[2].Load() != 2 {
					t.Fatal("timeout error cached")
				}
			}
		})
	}
	for _, options := range []cache.Options{{TTL: -1}, {MaxEntries: -1}, {MaxInflight: -1}, {LoadTimeout: -1}} {
		if _, err := NewCachedOrderReader(&orderCacheFake{}, options); err == nil {
			t.Fatal("negative option accepted")
		}
	}
	f := &orderCacheFake{}
	r := newOrderCacheTestReader(t, f, cache.Options{TTL: time.Second})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, op := range orderCacheReads {
		if err := op.read(ctx, r); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	}
	for i := range f.calls {
		if f.calls[i].Load() != 0 {
			t.Fatal("canceled caller reached repository")
		}
	}
}
