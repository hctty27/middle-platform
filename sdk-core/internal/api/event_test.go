package api

import (
    "context"
    "encoding/json"
    "errors"
    "net/http"
    "net/http/httptest"
    "strings"
    "testing"

    "github.com/hctty27/middle-platform/sdk-core/internal/auth"
    "github.com/hctty27/middle-platform/sdk-core/internal/platform"
    "github.com/hctty27/middle-platform/sdk-core/internal/service"
)

type fakeEventAPI struct {
    called int
    appID string
    env string
    body string
    err error
}
func (f *fakeEventAPI) Report(_ *http.Request, env, appID string, body []byte) error {
    f.called++
    f.appID, f.env, f.body = appID, env, string(body)
    return f.err
}
func runEventRequest(h http.Handler, body string) *httptest.ResponseRecorder {
    req := httptest.NewRequest(http.MethodPost, "/api/core/test/events", strings.NewReader(body))
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("x-app-id", "110006")
    recorder := httptest.NewRecorder()
    h.ServeHTTP(recorder, req)
    return recorder
}
func TestEventRouteAndStrictPayload(t *testing.T) {
    svc := &fakeEventAPI{}
    router := RouterWithEvents(&fakeFetcher{}, fakePinger{}, svc)
    body := `{"event":"login","ext":{"roleId":"r1"}}`
    response := runEventRequest(router, body)
    if response.Code != http.StatusOK || svc.called != 1 ||
        svc.env != "test" || svc.appID != "110006" || svc.body != body {
        t.Fatalf("上报参数错误: HTTP=%d service=%+v", response.Code, svc)
    }
    var result struct {
        Code int `json:"code"`
        Data bool `json:"data"`
    }
    if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || result.Code != 0 || !result.Data {
        t.Fatalf("成功响应不符合契约: %s %v", response.Body.String(), err)
    }
    for _, invalid := range []string{
        `{"event":"login"}`,
        `{"event":"login","event":"logout","ext":{}}`,
        `{"event":"login","ext":{},"unknown":1}`,
        `{"event":123,"ext":{}}`,
        `{"event":"login","ext":null}`,
    } {
        previous := svc.called
        response := runEventRequest(router, invalid)
        if response.Code != http.StatusBadRequest || svc.called != previous {
            t.Errorf("非法 Body 被接受: HTTP=%d Body=%s", response.Code, invalid)
        }
    }
}
func TestEventRouteErrorMapping(t *testing.T) {
    for _, tc := range []struct {
        err error
        want int
    }{
        {service.ErrInvalidEvent, 400},
        {auth.ErrUnauthorized, 401},
        {service.ErrServiceDisabled, 403},
        {platform.ErrEventEndpointMissing, 503},
        {platform.ErrEventRejected, 502},
        {errors.New("database unavailable"), 503},
    } {
        f := &fakeEventAPI{err: tc.err}
        r := runEventRequest(RouterWithEvents(&fakeFetcher{}, fakePinger{}, f),
            `{"event":"login","ext":{}}`)
        if r.Code != tc.want {
            t.Errorf("%v: got %d, want %d", tc.err, r.Code, tc.want)
        }
    }
}
func TestNoEventRouteInLegacyRouter(t *testing.T) {
    legacy := Router(&fakeFetcher{}, fakePinger{})
    response := runEventRequest(legacy, `{"event":"login","ext":{}}`)
    if response.Code != http.StatusNotFound {
        t.Fatalf("旧 Router 意外注册事件: %d", response.Code)
    }
}
var _ = context.Background
