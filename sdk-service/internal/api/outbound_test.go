package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"hitall.com/middle-platform/sdk-service/internal/model"
	"hitall.com/middle-platform/sdk-service/internal/platform"
	"hitall.com/middle-platform/sdk-service/internal/service"
	"hitall.com/middle-platform/shared/signature"
)

type fakeOutbound struct {
	events        int
	subscriptions int
	raw           string
	appID         string
	env           string
	async         bool
}

func (f *fakeOutbound) ReportEvent(_ *http.Request, env, appID string, body []byte, async bool) error {
	f.events++
	f.async = async
	f.env, f.appID, f.raw = env, appID, string(body)
	return nil
}
func (f *fakeOutbound) SendSubscribe(_ *http.Request, env, appID string, body []byte) error {
	f.subscriptions++
	f.env, f.appID, f.raw = env, appID, string(body)
	return nil
}

func TestEventReportMode(t *testing.T) {
	for _, tc := range []struct {
		name   string
		modes  []string
		status int
		calls  int
		async  bool
	}{
		{"default", nil, 200, 1, false},
		{"sync", []string{"sync"}, 200, 1, false},
		{"async", []string{"async"}, 202, 1, true},
		{"unknown", []string{"later"}, 400, 0, false},
		{"empty", []string{""}, 400, 0, false},
		{"repeated", []string{"sync", "async"}, 400, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			outbound := &fakeOutbound{}
			handler := RouterWithPublishers(&fakeChecker{}, fakeDB{}, nil, nil, nil, outbound, nil)
			req := httptest.NewRequest(http.MethodPost, "/api/service/events/test", strings.NewReader(`{"event":"login","ext":{}}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("x-app-id", "110006")
			for _, mode := range tc.modes {
				req.Header.Add("X-Report-Mode", mode)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tc.status || outbound.events != tc.calls || outbound.async != tc.async {
				t.Fatalf("status=%d calls=%d async=%v body=%s", rec.Code, outbound.events, outbound.async, rec.Body.String())
			}
		})
	}
}

type eventRoutes struct{}

func (eventRoutes) LookupService(_ context.Context, appID, env, _ string) (model.Target, error) {
	return model.Target{AppID: appID, Env: env, GameSecret: "game-secret", AppSecret: "platform-secret"}, nil
}

func TestAsyncEventReturnsBeforePlatformResponse(t *testing.T) {
	const payload = `{"event":"login","ext":{}}`
	received := make(chan string, 1)
	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		received <- string(body)
		<-release
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer upstream.Close()
	defer close(release)
	publisher, err := platform.NewPublisher(upstream.URL+"/report/event", "")
	if err != nil {
		t.Fatal(err)
	}
	outbound := &service.OutboundService{Routes: eventRoutes{}, Sender: publisher}
	handler := RouterWithPublishers(&fakeChecker{}, fakeDB{}, nil, nil, nil, outbound, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/api/service/events/test", strings.NewReader(payload))
	const appID, nonce = "110006", "0011223344556677"
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Report-Mode", "async")
	req.Header.Set("x-app-id", appID)
	req.Header.Set("x-timestamp", ts)
	req.Header.Set("x-nonce", nonce)
	req.Header.Set("x-signature", signature.SignGameRequest([]byte("game-secret"), req.Method, req.URL.EscapedPath(), appID, ts, nonce, []byte(payload)))

	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		cancel()
		done <- rec
	}()
	select {
	case rec := <-done:
		if rec.Code != http.StatusAccepted || !strings.Contains(rec.Body.String(), `"message":"accepted"`) {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
	case <-time.After(3 * time.Second):
		t.Fatal("异步接口未在平台响应前返回")
	}
	select {
	case body := <-received:
		if body != payload {
			t.Fatalf("平台收到的请求体: %s", body)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("平台未收到异步上报")
	}
}

func TestEventAndSubscribeRoutes(t *testing.T) {
	tests := []struct {
		name, path, body             string
		status, eventCalls, subCalls int
	}{
		{"事件正常", "/api/service/events/test", `{"event":"login","ext":{"roleId":"r1"}}`, 200, 1, 0},
		{"事件空对象", "/api/service/events/test", `{"event":"login","ext":{}}`, 200, 1, 0},
		{"事件缺失 ext", "/api/service/events/test", `{"event":"login"}`, 400, 0, 0},
		{"事件重复字段", "/api/service/events/test", `{"event":"a","event":"b","ext":{}}`, 400, 0, 0},
		{"事件非法字段", "/api/service/events/test", `{"event":"a","ext":{},"unexpected":1}`, 400, 0, 0},
		{"事件 ext 字符串", "/api/service/events/test", `{"event":"a","ext":"x"}`, 400, 0, 0},
		{"订阅只填必填", "/api/service/subscribe/test", `{"open_id":"u1","tpl_id":"tpl"}`, 200, 0, 1},
		{"订阅带微信模板", "/api/service/subscribe/test", `{"open_id":"u1","tpl_id":"tpl","data":"{\"thing1\":{\"value\":\"礼包\"}}"}`, 200, 0, 1},
		{"订阅缺模板", "/api/service/subscribe/test", `{"open_id":"u1"}`, 200, 0, 1},
		{"订阅旧格式", "/api/service/subscribe/test", ` {"templateId":"tpl","snOpenId":"u1","content":{"thing1":{"value":"礼包"}},"extend":{"gamePackageId":123}} `, 200, 0, 1},
		{"订阅 data 非字符串", "/api/service/subscribe/test", `{"open_id":"u1","tpl_id":"tpl","data":{"x":1}}`, 200, 0, 1},
		{"订阅 data 任意字符串", "/api/service/subscribe/test", `{"open_id":"u1","tpl_id":"tpl","data":"not-json"}`, 200, 0, 1},
		{"订阅重复字段", "/api/service/subscribe/test", `{"open_id":"u1","open_id":"u2","tpl_id":"tpl"}`, 200, 0, 1},
		{"订阅 ext 对象", "/api/service/subscribe/test", `{"open_id":"u1","tpl_id":"tpl","ext":{}}`, 200, 0, 1},
		{"订阅非法 JSON", "/api/service/subscribe/test", `{"broken":`, 400, 0, 0},
		{"订阅数组", "/api/service/subscribe/test", `[]`, 400, 0, 0},
		{"非法环境", "/api/service/subscribe/other", `{"open_id":"u1","tpl_id":"tpl"}`, 400, 0, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			outbound := &fakeOutbound{}
			handler := RouterWithPublishers(&fakeChecker{}, fakeDB{}, nil, nil, nil, outbound, outbound)
			req := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("x-app-id", "110006")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tc.status || outbound.events != tc.eventCalls || outbound.subscriptions != tc.subCalls {
				t.Fatalf("HTTP %d, calls=%d/%d, response=%s", rec.Code, outbound.events, outbound.subscriptions, rec.Body.String())
			}
			if tc.status == 200 && (outbound.raw != tc.body ||
				!strings.Contains(rec.Body.String(), `"data":true`)) {
				t.Fatalf("请求 Body 或返回格式错误: %s", rec.Body.String())
			}
		})
	}
	oversized := &fakeOutbound{}
	oversizedHandler := RouterWithPublishers(&fakeChecker{}, fakeDB{}, nil, nil, nil, oversized, oversized)
	huge := `{"event":"login","ext":{"value":"` + strings.Repeat("x", 65536) + `"}}`
	request := httptest.NewRequest(http.MethodPost, "/api/service/events/test", strings.NewReader(huge))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("x-app-id", "110006")
	response := httptest.NewRecorder()
	oversizedHandler.ServeHTTP(response, request)
	if response.Code != http.StatusRequestEntityTooLarge || oversized.events != 0 {
		t.Fatalf("超长事件请求未拒绝: HTTP=%d calls=%d", response.Code, oversized.events)
	}
	request = httptest.NewRequest(http.MethodPost, "/api/service/subscribe/test", strings.NewReader(huge))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("x-app-id", "110006")
	response = httptest.NewRecorder()
	oversizedHandler.ServeHTTP(response, request)
	if response.Code != http.StatusRequestEntityTooLarge || oversized.subscriptions != 0 {
		t.Fatalf("超长订阅请求未拒绝: HTTP=%d calls=%d", response.Code, oversized.subscriptions)
	}
	legacy := RouterWithServices(&fakeChecker{}, fakeDB{}, nil, nil)
	for _, path := range []string{"/api/service/events/test", "/api/service/subscribe/test"} {
		rec := httptest.NewRecorder()
		legacy.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("缺少业务依赖时不应注册路由: %s", path)
		}
	}
}
