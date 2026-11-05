package api

import (
    "errors"
    "net/http"
    "net/http/httptest"
    "strings"
    "testing"

    "github.com/hctty27/middle-platform/sdk-service/internal/model"
    "github.com/hctty27/middle-platform/sdk-service/internal/platform"
)

type fakeRoleController struct {
    calls int
    input model.RoleControlRequest
    err error
}

func (f *fakeRoleController) Execute(_ *http.Request, _, _ string, in model.RoleControlRequest) error {
    f.calls++
    f.input = in
    return f.err
}

func TestRoleControlRouter(t *testing.T) {
    cases := []struct {
        name, body string
        status, calls int
        duration *int64
    }{
        {"封号一小时", `{"type":"close","role_id":"r1","duration":3600}`, 200, 1, numberPtr(3600)},
        {"永久禁言", `{"type":"lock","open_id":"u1","duration":0}`, 200, 1, numberPtr(0)},
        {"解封不传时长", `{"type":"unClose","role_name":"测试角色"}`, 200, 1, nil},
        {"解禁言 IP", `{"type":"unLock","ip":"127.0.0.1"}`, 200, 1, nil},
        {"缺少类型", `{"role_id":"r1","duration":1}`, 400, 0, nil},
        {"无操作目标", `{"type":"close","server_id":"1000","duration":1}`, 400, 0, nil},
        {"封号缺时长", `{"type":"close","role_id":"r1"}`, 400, 0, nil},
        {"禁言缺时长", `{"type":"lock","role_id":"r1"}`, 400, 0, nil},
        {"负时长", `{"type":"close","role_id":"r1","duration":-1}`, 400, 0, nil},
        {"时长浮点数", `{"type":"close","role_id":"r1","duration":1.0}`, 400, 0, nil},
        {"时长字符串", `{"type":"close","role_id":"r1","duration":"1"}`, 400, 0, nil},
        {"时长空值", `{"type":"close","role_id":"r1","duration":null}`, 400, 0, nil},
        {"类型大小写", `{"type":"unlock","role_id":"r1"}`, 400, 0, nil},
        {"重复字段", `{"type":"close","role_id":"r1","role_id":"r2","duration":1}`, 400, 0, nil},
        {"非法字段", `{"type":"unClose","role_id":"r1","extra":1}`, 400, 0, nil},
        {"非法 IP", `{"type":"lock","ip":"invalid","duration":1}`, 400, 0, nil},
        {"错误 JSON 类型", `{"type":"close","role_id":7,"duration":1}`, 400, 0, nil},
        {"多个 JSON", `{"type":"unClose","role_id":"r1"}{}`, 400, 0, nil},
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            fake := &fakeRoleController{}
            req := httptest.NewRequest(http.MethodPost, "/api/service/role/control/test", strings.NewReader(tc.body))
            req.Header.Set("Content-Type", "application/json")
            req.Header.Set("x-app-id", "110006")
            rec := httptest.NewRecorder()
            RouterWithServices(&fakeChecker{}, fakeDB{}, nil, nil, fake).ServeHTTP(rec, req)
            if rec.Code != tc.status || fake.calls != tc.calls {
                t.Fatalf("状态=%d, 调用=%d, 响应=%s", rec.Code, fake.calls, rec.Body.String())
            }
            if tc.calls == 1 {
                if (fake.input.Duration == nil) != (tc.duration == nil) ||
                    (tc.duration != nil && *fake.input.Duration != *tc.duration) {
                    t.Fatalf("duration 丢失: %+v", fake.input)
                }
                if !strings.Contains(rec.Body.String(), `"data":null`) {
                    t.Fatalf("成功响应必须 data=null: %s", rec.Body.String())
                }
            }
        })
    }
}

func TestRoleControlErrorMapping(t *testing.T) {
    cases := []struct { err error; status int }{
        {model.ErrDisabled, 403},
        {model.ErrNotFound, 404},
        {platform.ErrUnauthorized, 401},
        {model.ErrUpstream, 502},
        {model.ErrInvalidConfig, 500},
        {errors.New("unavailable"), 503},
    }
    for _, tc := range cases {
        fake := &fakeRoleController{err: tc.err}
        req := httptest.NewRequest(http.MethodPost, "/api/service/role/control/prod",
            strings.NewReader(`{"type":"unClose","role_id":"r1"}`))
        req.Header.Set("Content-Type", "application/json")
        req.Header.Set("x-app-id", "110006")
        rec := httptest.NewRecorder()
        RouterWithServices(&fakeChecker{}, fakeDB{}, nil, nil, fake).ServeHTTP(rec, req)
        if rec.Code != tc.status {
            t.Fatalf("错误 %v 映射到 %d, 预期 %d", tc.err, rec.Code, tc.status)
        }
    }
}

func numberPtr(value int64) *int64 { return &value }
