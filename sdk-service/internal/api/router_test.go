package api

import (
    "context"
    "net/http"
    "net/http/httptest"
    "strings"
    "testing"

    "github.com/hctty27/middle-platform/sdk-service/internal/model"
)

type fakeDB struct{}
func (fakeDB) PingContext(context.Context) error { return nil }

type fakeChecker struct { exists bool; calls int }
func (f *fakeChecker) Check(_ *http.Request, _, _ string, _ model.UserCheckRequest) (model.UserCheckResult, error) {
    f.calls++
    return model.UserCheckResult{Exists: f.exists}, nil
}

func TestUserCheckRouter(t *testing.T) {
    cases := []struct{
        name, body string
        exists bool
        status int
        text string
        calls int
    }{
        {"存在", `{"open_id":"x"}`, true, 200, `"data":true`, 1},
        {"不存在", `{"open_id":"x"}`, false, 200, `"code":1021003`, 1},
        {"非法字段", `{"open_id":"x","extra":1}`, true, 400, `"code":400`, 0},
        {"重复字段", `{"open_id":"x","open_id":"y"}`, true, 400, `"code":400`, 0},
        {"空值", `{"open_id":""}`, true, 400, `"code":400`, 0},
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            checker := &fakeChecker{exists: tc.exists}
            req := httptest.NewRequest(http.MethodPost, "/api/service/user/check/prod",
                strings.NewReader(tc.body))
            req.Header.Set("Content-Type", "application/json")
            req.Header.Set("x-app-id", "110006")
            rec := httptest.NewRecorder()
            Router(checker, fakeDB{}).ServeHTTP(rec, req)
            if rec.Code != tc.status || !strings.Contains(rec.Body.String(), tc.text) ||
                checker.calls != tc.calls {
                t.Fatalf("响应不符合预期: %d, %s, calls=%d", rec.Code, rec.Body.String(), checker.calls)
            }
        })
    }
}
