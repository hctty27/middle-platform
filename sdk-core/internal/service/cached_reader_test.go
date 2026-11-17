package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"hitall.com/middle-platform/sdk-core/internal/model"
	"hitall.com/middle-platform/shared/cache"
)

type cachedPublicFake struct {
	calls int
	value model.ConfigResponse
	err   error
}

func (f *cachedPublicFake) LookupPublicConfig(_ context.Context, appID, env string) (model.ConfigResponse, error) {
	f.calls++
	return f.value, f.err
}

func TestCachedReaderPublicCloneAndKeys(t *testing.T) {
	key := "original"
	f := &cachedPublicFake{value: model.ConfigResponse{SnKey: "sn", ZoneKey: "zone", TsKey: &key}}
	r, err := NewCachedReader(f, cache.Options{TTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	ctx := context.Background()
	a, err := r.LookupPublicConfig(ctx, "a", "prod")
	if err != nil {
		t.Fatal(err)
	}
	*a.TsKey = "caller change"
	key = "source change"
	b, err := r.LookupPublicConfig(ctx, "a", "prod")
	if err != nil || *b.TsKey != "original" || b.SnKey != "sn" || b.ZoneKey != "zone" || f.calls != 1 {
		t.Fatalf("%+v %v calls %d", b, err, f.calls)
	}
	_, _ = r.LookupPublicConfig(ctx, "a", "test")
	_, _ = r.LookupPublicConfig(ctx, "b", "prod")
	if f.calls != 3 || r.Stats().Entries != 3 {
		t.Fatalf("calls %d stats %+v", f.calls, r.Stats())
	}
	// Nil remains nil instead of manufacturing an optional key.
	f.value.TsKey = nil
	v, err := r.LookupPublicConfig(ctx, "c", "review")
	if err != nil || v.TsKey != nil {
		t.Fatalf("%+v %v", v, err)
	}
}

func TestCachedReaderPublicDisabledAndErrors(t *testing.T) {
	for _, ttl := range []time.Duration{0, time.Hour} {
		f := &cachedPublicFake{err: model.ErrInvalidRequest}
		r, err := NewCachedReader(f, cache.Options{TTL: ttl})
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 2; i++ {
			if _, err := r.LookupPublicConfig(context.Background(), "a", "prod"); !errors.Is(err, model.ErrInvalidRequest) {
				t.Fatal(err)
			}
		}
		if f.calls != 2 {
			t.Fatal(f.calls)
		}
		f.err = nil
		for i := 0; i < 2; i++ {
			if _, err := r.LookupPublicConfig(context.Background(), "a", "prod"); err != nil {
				t.Fatal(err)
			}
		}
		want := 3
		if ttl == 0 {
			want = 4
		}
		if f.calls != want {
			t.Fatalf("ttl %v calls %d", ttl, f.calls)
		}
		r.Close()
	}
	if _, err := NewCachedReader(&cachedPublicFake{}, cache.Options{TTL: -1}); err == nil {
		t.Fatal("negative TTL accepted")
	}
}

func BenchmarkCachedReaderPublic(b *testing.B) {
	for _, ttl := range []time.Duration{0, time.Hour} {
		b.Run(ttl.String(), func(b *testing.B) {
			key := "ts"
			f := &cachedPublicFake{value: model.ConfigResponse{TsKey: &key}}
			r, _ := NewCachedReader(f, cache.Options{TTL: ttl})
			defer r.Close()
			ctx := context.Background()
			_, _ = r.LookupPublicConfig(ctx, "a", "prod")
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := r.LookupPublicConfig(ctx, "a", "prod"); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestCachedJSONSharesTTLAndValue(t *testing.T) {
	key := "<>&\\"
	f := &cachedPublicFake{value: model.ConfigResponse{SnKey: "sn", ZoneKey: "zone", TsKey: &key}}
	r, err := NewCachedReader(f, cache.Options{TTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	ctx := context.Background()
	first, err := r.LookupPublicConfigJSON(ctx, "app", "prod")
	if err != nil {
		t.Fatal(err)
	}
	value, err := r.LookupPublicConfig(ctx, "app", "prod")
	if err != nil {
		t.Fatal(err)
	}
	expected, err := model.EncodePublicConfig(value)
	if err != nil || first != expected || f.calls != 1 {
		t.Fatalf("cached response mismatch: %q %q calls=%d err=%v", first, expected, f.calls, err)
	}
	*value.TsKey = "mutated"
	again, err := r.LookupPublicConfigJSON(ctx, "app", "prod")
	if err != nil || again != first {
		t.Fatalf("cached JSON mutated: %q %v", again, err)
	}
}
