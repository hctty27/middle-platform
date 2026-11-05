package api

import (
    "net/http"
    "net/http/httptest"
    "strings"
    "testing"
)

type fakeOutbound struct {
    events int
    subscriptions int
    raw string
    appID string
    env string
}
func (f *fakeOutbound) ReportEvent(_ *http.Request, env, appID string, body []byte) error {
    f.events++
    f.env, f.appID, f.raw = env, appID, string(body)
    return nil
}
func (f *fakeOutbound) SendSubscribe(_ *http.Request, env, appID string, body []byte) error {
    f.subscriptions++
    f.env, f.appID, f.raw = env, appID, string(body)
    return nil
}

func TestEventAndSubscribeRoutes(t *testing.T) {
    tests := []struct{
        name, path, body string
        status, eventCalls, subCalls int
    }{
        {"事件正常", "/api/service/events/test", `{"event":"login","ext":{"roleId":"r1"}}`, 200, 1, 0},
        {"事件空对象", "/api/service/events/test", `{"event":"login","ext":{}}`, 200, 1, 0},
        {"事件缺失 ext", "/api/service/events/test", `{"event":"login"}`, 400, 0, 0},
        {"事件重复字段", "/api/service/events/test", `{"event":"a","event":"b","ext":{}}`, 400, 0, 0},
        {"事件非法字段", "/api/service/events/test", `{"event":"a","ext":{},"unexpected":1}`, 400, 0, 0},
        {"事件 ext 字符串", "/api/service/events/test", `{"event":"a","ext":"x"}`, 400, 0, 0},
        {"订阅只填必填", "/api/service/subscribe/test", `{"open_id":"u1","tpl_id":"tpl"}`, 200, 0, 1},
        {"订阅带微信模板", "/api/service/subscribe/test", `{"open_id":"u1","tpl_id":"tpl","data":"{\\"thing1\\":{\\"value\\":\\"礼包\\"}}"}`, 200, 0, 1},
        {"订阅缺模板", "/api/service/subscribe/test", `{"open_id":"u1"}`, 400, 0, 0},
        {"订阅 data 非字符串", "/api/service/subscribe/test", `{"open_id":"u1","tpl_id":"tpl","data":{"x":1}}`, 400, 0, 0},
        {"订阅 data 无效 JSON", "/api/service/subscribe/test", `{"open_id":"u1","tpl_id":"tpl","data":"not-json"}`, 400, 0, 0},
        {"订阅重复字段", "/api/service/subscribe/test", `{"open_id":"u1","open_id":"u2","tpl_id":"tpl"}`, 400, 0, 0},
        {"订阅 ext 错误", "/api/service/subscribe/test", `{"open_id":"u1","tpl_id":"tpl","ext":{}}`, 400, 0, 0},
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
    legacy := RouterWithServices(&fakeChecker{}, fakeDB{}, nil, nil)
    for _, path := range []string{"/api/service/events/test", "/api/service/subscribe/test"} {
        rec := httptest.NewRecorder()
        legacy.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, nil))
        if rec.Code != http.StatusNotFound {
            t.Fatalf("缺少业务依赖时不应注册路由: %s", path)
        }
    }
}
