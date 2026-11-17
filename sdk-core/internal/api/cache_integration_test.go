package api

import (
	"context"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"hitall.com/middle-platform/sdk-core/internal/model"
	"hitall.com/middle-platform/sdk-core/internal/service"
	"hitall.com/middle-platform/shared/cache"
)

type publicReaderFunc func(context.Context, string, string) (model.ConfigResponse, error)

func (f publicReaderFunc) LookupPublicConfig(ctx context.Context, app, env string) (model.ConfigResponse, error) {
	return f(ctx, app, env)
}

func TestPublicCacheHTTPIntegration(t *testing.T) {
	for _, ttl := range []time.Duration{0, time.Second} {
		t.Run(ttl.String(), func(t *testing.T) {
			var calls atomic.Int32
			reader, err := service.NewCachedReader(publicReaderFunc(func(_ context.Context, app, env string) (model.ConfigResponse, error) {
				calls.Add(1)
				if app != "app" || env != "prod" {
					t.Errorf("wrong key: %s/%s", app, env)
				}
				return model.ConfigResponse{SnKey: "sn", ZoneKey: "zone"}, nil
			}), cache.Options{TTL: ttl})
			if err != nil {
				t.Fatal(err)
			}
			defer reader.Close()
			h := RouterWithOptions(service.NewConfigService(reader), fakePinger{}, RouterOptions{RequestTimeout: time.Second})
			for range 2 {
				rr := httptest.NewRecorder()
				h.ServeHTTP(rr, httptest.NewRequest("GET", "/api/core/config/prod/app", nil))
				if rr.Code != 200 || rr.Header().Get("Cache-Control") != "no-store" || rr.Body.String() != `{"code":0,"message":"success","data":{"snKey":"sn","zoneKey":"zone"}}`+"\n" {
					t.Fatalf("unexpected response: %d %s", rr.Code, rr.Body.String())
				}
			}
			want := int32(1)
			if ttl == 0 {
				want = 2
			}
			if calls.Load() != want {
				t.Fatalf("loads=%d want=%d", calls.Load(), want)
			}
		})
	}
}
