package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hctty27/middle-platform/sdk-core/internal/model"
)

type fakeOrderRoutes struct {
	verifiedEnv, verifiedAppID string
	registeredEnv, registeredAppID string
	registeredOrder model.RegisterOrder
	callbackAppID string
	callback model.PaymentCallback
}

func (f *fakeOrderRoutes) VerifyRegistration(_ *http.Request, _ []byte, env, appID string) error {
	f.verifiedEnv, f.verifiedAppID = env, appID
	return nil
}

func (f *fakeOrderRoutes) Register(_ context.Context, env, appID string, order model.RegisterOrder) error {
	f.registeredEnv, f.registeredAppID, f.registeredOrder = env, appID, order
	return nil
}

func (f *fakeOrderRoutes) Receive(_ context.Context, appID string, _ http.Header, callback model.PaymentCallback) (string, error) {
	f.callbackAppID, f.callback = appID, callback
	return "0123456789abcdef", nil
}

func TestBusinessRoutesUseTrailingParameters(t *testing.T) {
	fetcher := &fakeFetcher{result: map[string]any{"snKey": "110006"}}
	events := &fakeEventAPI{}
	orders := &fakeOrderRoutes{}
	router := RouterWithEvents(fetcher, fakePinger{}, events, orders)

	tests := []struct {
		name, method, path, body string
	}{
		{"config", http.MethodGet, "/api/core/config/test/110006", ""},
		{"register", http.MethodPost, "/api/core/orders/test/110006", `{"game_order_id":"G100","amount":600,"attach":""}`},
		{"payment", http.MethodPost, "/api/core/pay/notify/110006", `{"order_no":"P100","game_order_id":"G100","amount":600,"attach":"","paid_at":"2026-11-04T00:00:00Z"}`},
		{"event", http.MethodPost, "/api/core/events/test", `{"event":"login","ext":{"roleId":"1"}}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			req.Header.Set("x-app-id", "110006")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("%s %s: HTTP %d: %s", tc.method, tc.path, rec.Code, rec.Body.String())
			}
			if tc.name == "payment" && rec.Body.String() != "0123456789abcdef" {
				t.Fatalf("平台回调 nonce 错误: %q", rec.Body.String())
			}
		})
	}
	if fetcher.env != "test" || fetcher.appID != "110006" {
		t.Fatalf("配置查询参数错误: env=%s appID=%s", fetcher.env, fetcher.appID)
	}
	if orders.verifiedEnv != "test" || orders.verifiedAppID != "110006" ||
		orders.registeredEnv != "test" || orders.registeredAppID != "110006" ||
		orders.registeredOrder.GameOrderID != "G100" {
		t.Fatalf("订单登记参数错误: %+v", orders)
	}
	if orders.callbackAppID != "110006" || orders.callback.OrderNo != "P100" {
		t.Fatalf("平台回调参数错误: %+v", orders)
	}
	if events.called != 1 || events.env != "test" || events.appID != "110006" {
		t.Fatalf("事件上报参数错误: %+v", events)
	}
}

func TestOldBusinessRoutesReturnNotFound(t *testing.T) {
	router := RouterWithEvents(&fakeFetcher{}, fakePinger{}, &fakeEventAPI{}, &fakeOrderRoutes{})
	for _, tc := range []struct { method, path string }{
		{http.MethodGet, "/api/core/test/110006"},
		{http.MethodPost, "/api/core/test/110006/orders"},
		{http.MethodPost, "/api/core/110006/pay/notify"},
		{http.MethodPost, "/api/core/test/events"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))
			if rec.Code != http.StatusNotFound {
				t.Fatalf("旧路径应返回 404: %s %s => %d", tc.method, tc.path, rec.Code)
			}
		})
	}
}
