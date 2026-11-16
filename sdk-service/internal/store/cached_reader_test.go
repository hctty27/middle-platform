package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"hitall.com/middle-platform/sdk-service/internal/model"
	"hitall.com/middle-platform/shared/cache"
)

type cachedRouteFake struct {
	calls int
	value model.Target
	err   error
}

func (f *cachedRouteFake) Lookup(_ context.Context, appID, env string) (model.Target, error) {
	f.calls++
	v := f.value
	v.AppID = appID
	v.Env = env
	v.GameCode = "user"
	return v, f.err
}
func (f *cachedRouteFake) LookupService(_ context.Context, appID, env, code string) (model.Target, error) {
	f.calls++
	v := f.value
	v.AppID = appID
	v.Env = env
	v.GameCode = code
	return v, f.err
}

func TestCachedReaderNamespacesAndValueIsolation(t *testing.T) {
	f := &cachedRouteFake{value: model.Target{SkipUserCheck: true, AppSecret: "app", GameSecret: "game", AdapterName: "adapter", RoleQueryURL: "https://example.com"}}
	r, err := NewCachedReader(f, cache.Options{TTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	ctx := context.Background()
	a, err := r.Lookup(ctx, "a", "prod")
	if err != nil {
		t.Fatal(err)
	}
	a.SkipUserCheck = false
	a.AppSecret = "changed"
	f.value.GameSecret = "rotated"
	v, err := r.Lookup(ctx, "a", "prod")
	if err != nil || !v.SkipUserCheck || v.AppSecret != "app" || v.GameSecret != "game" || f.calls != 1 {
		t.Fatalf("cached value mismatch, err %v calls %d", err, f.calls)
	}
	// Even a service code equal to the user-check namespace cannot alias Lookup.
	v, err = r.LookupService(ctx, "a", "prod", "user_check")
	if err != nil || v.GameCode != "user_check" || f.calls != 2 {
		t.Fatalf("namespace collision: %v calls %d", err, f.calls)
	}
	for _, k := range []routeCacheKey{{"service", "a", "prod", "role_query"}, {"service", "a", "prod", "role_control"}, {"service", "a", "test", "role_query"}, {"service", "b", "prod", "role_query"}} {
		_, _ = r.LookupService(ctx, k.appID, k.env, k.service)
	}
	if f.calls != 6 || r.Stats().Entries != 6 {
		t.Fatalf("calls %d stats %+v", f.calls, r.Stats())
	}
	_, _ = r.LookupService(ctx, "a", "prod", "role_query")
	if f.calls != 6 {
		t.Fatal(f.calls)
	}
}

func TestCachedReaderSharedCapacity(t *testing.T) {
	f := &cachedRouteFake{}
	r, err := NewCachedReader(f, cache.Options{TTL: time.Hour, MaxEntries: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	ctx := context.Background()
	_, _ = r.Lookup(ctx, "a", "prod")
	_, _ = r.LookupService(ctx, "a", "prod", "role_query")
	_, _ = r.Lookup(ctx, "a", "prod")
	if f.calls != 3 || r.Stats().Entries != 1 || r.Stats().Evictions != 2 {
		t.Fatalf("calls %d stats %+v", f.calls, r.Stats())
	}
}

func TestCachedReaderDisabledAndErrorSemantics(t *testing.T) {
	for _, ttl := range []time.Duration{0, time.Hour} {
		f := &cachedRouteFake{}
		r, err := NewCachedReader(f, cache.Options{TTL: ttl})
		if err != nil {
			t.Fatal(err)
		}
		for _, sourceErr := range []error{model.ErrNotFound, model.ErrDisabled, model.ErrInvalidConfig} {
			f.err = sourceErr
			for i := 0; i < 2; i++ {
				if _, err := r.Lookup(context.Background(), "a", "prod"); !errors.Is(err, sourceErr) {
					t.Fatal(err)
				}
				if _, err := r.LookupService(context.Background(), "a", "prod", "role_query"); !errors.Is(err, sourceErr) {
					t.Fatal(err)
				}
			}
		}
		if f.calls != 12 {
			t.Fatal(f.calls)
		}
		f.err = nil
		_, _ = r.Lookup(context.Background(), "a", "prod")
		f.value.SkipUserCheck = true
		v, err := r.Lookup(context.Background(), "a", "prod")
		if err != nil || v.SkipUserCheck != (ttl == 0) {
			t.Fatalf("switch semantics ttl %v err %v", ttl, err)
		}
		if ttl == 0 && r.Stats() != (cache.Stats{}) {
			t.Fatal(r.Stats())
		}
		r.Close()
		if _, err := r.Lookup(context.Background(), "a", "prod"); !errors.Is(err, cache.ErrClosed) {
			t.Fatal(err)
		}
	}
	if _, err := NewCachedReader(&cachedRouteFake{}, cache.Options{MaxInflight: -1}); err == nil {
		t.Fatal("negative limit accepted")
	}
}

func BenchmarkCachedReaderRoutes(b *testing.B) {
	for _, ttl := range []time.Duration{0, time.Hour} {
		b.Run(ttl.String(), func(b *testing.B) {
			r, _ := NewCachedReader(&cachedRouteFake{}, cache.Options{TTL: ttl})
			defer r.Close()
			ctx := context.Background()
			_, _ = r.LookupService(ctx, "a", "prod", "role_query")
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := r.LookupService(ctx, "a", "prod", "role_query"); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
