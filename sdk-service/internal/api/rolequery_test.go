package api

import (
    "net/http"
    "net/http/httptest"
    "strings"
    "testing"

    "github.com/hctty27/middle-platform/sdk-service/internal/model"
)

type fakeRoleQuerier struct {
    calls int
    result []model.Role
    err error
}

func (f *fakeRoleQuerier) Query(_ *http.Request, _, _ string, _ model.RoleQueryRequest) ([]model.Role, error) {
    f.calls++
    return f.result, f.err
}

func TestRoleQueryRouter(t *testing.T) {
    tests := []struct {
        name, body string
        code, calls int
        snippet string
    }{
        {"按用户查询", `{"open_id":"u1"}`, 200, 1, `"data":[]`},
        {"按角色查询", `{"role_id":"r1","extension":"channel"}`, 200, 1, `"code":0`},
        {"重复字段", `{"role_id":"r1","role_id":"r2"}`, 400, 0, `"code":400`},
        {"未知字段", `{"role_id":"r1","foo":"x"}`, 400, 0, `"code":400`},
        {"缺失查询条件", `{"extension":"ext"}`, 400, 0, `"code":400`},
        {"非法类型", `{"role_id":123}`, 400, 0, `"code":400`},
        {"空值", `{"role_id":null}`, 400, 0, `"code":400`},
        {"非对象", `[]`, 400, 0, `"code":400`},
        {"额外 JSON", `{"role_id":"r1"}{"role_id":"r2"}`, 400, 0, `"code":400`},
    }
    for _, tc := range tests {
        t.Run(tc.name, func(t *testing.T) {
            fake := &fakeRoleQuerier{}
            req := httptest.NewRequest(http.MethodPost, "/api/service/role/query/test", strings.NewReader(tc.body))
            req.Header.Set("Content-Type", "application/json")
            req.Header.Set("x-app-id", "110006")
            rec := httptest.NewRecorder()
            Router(&fakeChecker{}, fakeDB{}, fake).ServeHTTP(rec, req)
            if rec.Code != tc.code || fake.calls != tc.calls || !strings.Contains(rec.Body.String(), tc.snippet) {
                t.Fatalf("状态=%d, 调用=%d, 响应=%s", rec.Code, fake.calls, rec.Body.String())
            }
        })
    }
}
