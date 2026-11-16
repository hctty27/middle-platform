package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"hitall.com/middle-platform/sdk-service/internal/model"
)

type timeoutChecker struct{ run func(context.Context) error }

func (s timeoutChecker) Check(r *http.Request, _, _ string, _ model.UserCheckRequest) (model.UserCheckResult, error) {
	err := s.run(r.Context())
	return model.UserCheckResult{Exists: true}, err
}
func TestServiceDoesNotRejectConcurrentRequests(t *testing.T) {
	const count = 16
	entered, release := make(chan struct{}, count), make(chan struct{})
	checker := timeoutChecker{run: func(ctx context.Context) error {
		if _, ok := ctx.Deadline(); !ok {
			t.Error("missing deadline")
		}
		entered <- struct{}{}
		<-release
		return nil
	}}
	h := RouterWithOptions(checker, fakeDB{}, nil, nil, nil, nil, nil, RouterOptions{RequestTimeout: 5 * time.Second})
	var wg sync.WaitGroup
	defer func() { close(release); wg.Wait() }()
	for range count {
		wg.Go(func() {
			r := httptest.NewRequest("POST", "/api/service/user/check/prod", strings.NewReader(`{"open_id":"x"}`))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("x-app-id", "app")
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, r)
			if rr.Code != 200 || rr.Header().Get("Retry-After") != "" {
				t.Errorf("request rejected: %d %s", rr.Code, rr.Body.String())
			}
		})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for range count {
		select {
		case <-entered:
		case <-ctx.Done():
			t.Fatal("service still enforces concurrency quota")
		}
	}
}
func TestServiceRequestDeadlinePropagates(t *testing.T) {
	checker := timeoutChecker{run: func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }}
	h := RouterWithOptions(checker, fakeDB{}, nil, nil, nil, nil, nil, RouterOptions{RequestTimeout: time.Millisecond})
	r := httptest.NewRequest("POST", "/api/service/user/check/prod", strings.NewReader(`{"open_id":"x"}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("x-app-id", "app")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, r)
	if rr.Code != 503 || rr.Header().Get("Retry-After") != "" {
		t.Fatalf("unexpected response: %d %s", rr.Code, rr.Body.String())
	}
}
