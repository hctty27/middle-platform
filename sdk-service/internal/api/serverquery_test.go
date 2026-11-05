package api

import (
    "net/http"
    "net/http/httptest"
    "strings"
    "testing"

    "github.com/hctty27/middle-platform/sdk-service/internal/model"
)

type fakeServerQuerier struct {
    calls int
    input model.ServerQueryRequest
    data  model.ServerQueryResult
}

func (f *fakeServerQuerier) Query(_ *http.Request, _, _ string, in model.ServerQueryRequest) (model.ServerQueryResult, error) {
    f.calls++
    f.input = in
    return f.data, nil
}

func TestServerQueryRouter(t *testing.T) {
    cases := []struct {
        name, body string
        status, calls, pageSize int
        provided bool
    }{
        {"默认每页条数", `{"page":1}`, 200, 1, 1000, false},
        {"按原区服查询", `{"original_server_id":"1000","page":2,"page_size":100,"zonekey":"x"}`, 200, 1, 100, true},
        {"按当前区服查询", `{"current_server_id":"2000","page":1}`, 200, 1, 1000, false},
        {"缺少页码", `{"page_size":10}`, 400, 0, 0, false},
        {"负数页码", `{"page":-1}`, 400, 0, 0, false},
        {"零页码", `{"page":0}`, 400, 0, 0, false},
        {"每页超限", `{"page":1,"page_size":1001}`, 400, 0, 0, false},
        {"每页数字字符串", `{"page":1,"page_size":"10"}`, 400, 0, 0, false},
        {"小数页码", `{"page":1.5}`, 400, 0, 0, false},
        {"科学计数法", `{"page":1e2}`, 400, 0, 0, false},
        {"无效区服类型", `{"page":1,"current_server_id":7}`, 400, 0, 0, false},
        {"重复页码", `{"page":1,"page":2}`, 400, 0, 0, false},
        {"非法字段", `{"page":1,"other":true}`, 400, 0, 0, false},
        {"第二个 JSON 对象", `{"page":1}{"page":2}`, 400, 0, 0, false},
        {"数组请求", `[]`, 400, 0, 0, false},
        {"空值", `{"page":null}`, 400, 0, 0, false},
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            fake := &fakeServerQuerier{}
            req := httptest.NewRequest(http.MethodPost, "/api/service/server/query/test", strings.NewReader(tc.body))
            req.Header.Set("Content-Type", "application/json")
            req.Header.Set("x-app-id", "110006")
            rec := httptest.NewRecorder()
            RouterWithServices(&fakeChecker{}, fakeDB{}, nil, fake).ServeHTTP(rec, req)
            if rec.Code != tc.status || fake.calls != tc.calls {
                t.Fatalf("状态=%d, 调用=%d, 响应=%s", rec.Code, fake.calls, rec.Body.String())
            }
            if tc.calls == 1 {
                if fake.input.PageSize != tc.pageSize || fake.input.PageSizeProvided != tc.provided {
                    t.Fatalf("分页参数错误: %+v", fake.input)
                }
                if !strings.Contains(rec.Body.String(), `"total":0,"list":[]`) {
                    t.Fatalf("空结果结构错误: %s", rec.Body.String())
                }
            }
        })
    }
}

func TestServerQuerySignatureValues(t *testing.T) {
    input := model.ServerQueryRequest{Page: 1, PageSize: 1000, Zonekey: "channel"}
    values := input.SignatureValues()
    if len(values) != 4 || values[2] != "1" || values[3] != "channel" {
        t.Fatalf("平台入站只签实际提供的参数: %#v", values)
    }
    forwarded := input.ForwardSignatureValues()
    if len(forwarded) != 5 || forwarded[3] != "1000" {
        t.Fatalf("转发默认 page_size 必须参与签名: %#v", forwarded)
    }
    input.PageSizeProvided = true
    if values = input.SignatureValues(); len(values) != 5 || values[3] != "1000" {
        t.Fatalf("显式 page_size 必须参与签名: %#v", values)
    }
}
