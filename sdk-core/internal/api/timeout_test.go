package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"hitall.com/middle-platform/sdk-core/internal/model"
	"hitall.com/middle-platform/sdk-core/internal/service"
)

type timeoutOrders struct{ run func(context.Context) error }

func (s timeoutOrders) VerifyRegistration(r *http.Request, _ []byte, _, _ string) (service.VerifiedRegistration, error) {
	return service.VerifiedRegistration{}, s.run(r.Context())
}
func (timeoutOrders) Register(context.Context, string, string, model.RegisterOrder, service.VerifiedRegistration) error {
	return nil
}
func (s timeoutOrders) Receive(ctx context.Context, _ string, _ http.Header, _ model.PaymentCallback) (string, error) {
	if err := s.run(ctx); err != nil {
		return "", err
	}
	return "committed-nonce", nil
}

func TestOrderRoutesDoNotRejectConcurrentRequests(t *testing.T) {
	for _, tc := range []struct{ path, body string }{
		{"/api/core/orders/prod/app", `{"game_order_id":"g","amount":1,"attach":""}`},
		{"/api/core/pay/notify/app", `{"order_no":"p","game_order_id":"g","amount":1,"attach":"","paid_at":"2026-11-16T00:00:00Z"}`},
	} {
		t.Run(tc.path, func(t *testing.T) {
			const count = 16
			entered, release := make(chan struct{}, count), make(chan struct{})
			svc := timeoutOrders{run: func(ctx context.Context) error {
				if _, ok := ctx.Deadline(); !ok {
					t.Error("missing deadline")
				}
				entered <- struct{}{}
				<-release
				return nil
			}}
			h := RouterWithOptions(&fakeFetcher{}, fakePinger{}, RouterOptions{RequestTimeout: 5 * time.Second}, svc)
			var wg sync.WaitGroup
			defer func() { close(release); wg.Wait() }()
			for range count {
				wg.Go(func() {
					rr := httptest.NewRecorder()
					h.ServeHTTP(rr, httptest.NewRequest("POST", tc.path, strings.NewReader(tc.body)))
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
					t.Fatal("route still enforces concurrency quota")
				}
			}
		})
	}
}

func TestPaymentTimeoutDoesNotAcknowledgeFailure(t *testing.T) {
	svc := timeoutOrders{run: func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }}
	h := RouterWithOptions(&fakeFetcher{}, fakePinger{}, RouterOptions{RequestTimeout: time.Millisecond}, svc)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("POST", "/api/core/pay/notify/app", strings.NewReader(`{"order_no":"p","game_order_id":"g","amount":1,"attach":"","paid_at":"2026-11-16T00:00:00Z"}`)))
	if rr.Code != 503 || strings.Contains(rr.Body.String(), "committed-nonce") || rr.Header().Get("Retry-After") != "" {
		t.Fatalf("false success or proactive rejection: %d %s", rr.Code, rr.Body.String())
	}
}
