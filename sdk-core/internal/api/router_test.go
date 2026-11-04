package api

import (
    "context"
    "encoding/json"
    "errors"
    "net/http"
    "net/http/httptest"
    "reflect"
    "testing"

    "github.com/hctty27/middle-platform/sdk-core/internal/model"
)

type fakeFetcher struct {
    result map[string]any
    err error
    env string
    appID string
}

func (f *fakeFetcher) Get(_ context.Context,env,appID string) (map[string]any,error) {
    f.env,f.appID=env,appID
    return f.result,f.err
}

type fakePinger struct { err error }
func (p fakePinger) PingContext(context.Context) error { return p.err }

func TestCoreRouteUsesMappedResponseData(t *testing.T) {
    data:=map[string]any{"snKey":"a1","zoneKey":"s1","tsKey":"secretless"}
    f:=&fakeFetcher{result:data}
    rec:=httptest.NewRecorder()
    Router(f,fakePinger{}).ServeHTTP(rec,httptest.NewRequest(http.MethodGet,"/api/core/config/prod/app",nil))
    if rec.Code!=http.StatusOK || f.env!="prod" || f.appID!="app" {
        t.Fatalf("status=%d env=%s appID=%s",rec.Code,f.env,f.appID)
    }
    var response struct {
        Code int `json:"code"`
        Message string `json:"message"`
        Data map[string]any `json:"data"`
    }
    if err:=json.Unmarshal(rec.Body.Bytes(),&response);err!=nil { t.Fatal(err) }
    if response.Code!=0 || response.Message!="success" || !reflect.DeepEqual(response.Data,data) {
        t.Fatalf("response: %+v",response)
    }
    if rec.Header().Get("Cache-Control")!="no-store" { t.Fatal("Cache-Control should be no-store") }
    if _,ok:=response.Data["regions"];ok { t.Fatal("legacy regions field leaked") }
    if _,ok:=response.Data["config"];ok { t.Fatal("raw config leaked") }
}

func TestCoreRouteReturnsSuccessWithOmittedFields(t *testing.T) {
    f := &fakeFetcher{result: map[string]any{
        "snKey": "app1001",
        "zoneKey": "group1001",
    }}
    rec := httptest.NewRecorder()
    Router(f, fakePinger{}).ServeHTTP(rec,
        httptest.NewRequest(http.MethodGet, "/api/core/config/prod/app1001", nil))
    if rec.Code != http.StatusOK { t.Fatalf("HTTP %d: %s", rec.Code, rec.Body.String()) }
    var result struct {
        Code int `json:"code"`
        Message string `json:"message"`
        Data map[string]any `json:"data"`
    }
    if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil { t.Fatal(err) }
    if result.Code != 0 || result.Message != "success" { t.Fatalf("response: %+v", result) }
    if _, exists := result.Data["tsKey"]; exists { t.Fatalf("unconfigured tsKey must be omitted: %+v", result.Data) }
}

func TestConfigErrors(t *testing.T) {
    cases:=[]struct{err error;want int}{
        {model.ErrInvalidRequest,400},
        {model.ErrNotFound,404},
        {model.ErrAmbiguous,409},
        {model.ErrInvalidConfig,500},
        {errors.New("database unavailable"),503},
    }
    for _,tc:=range cases {
        rec:=httptest.NewRecorder()
        Router(&fakeFetcher{err:tc.err},fakePinger{}).ServeHTTP(rec,
            httptest.NewRequest(http.MethodGet,"/api/core/config/review/app",nil))
        if rec.Code!=tc.want { t.Errorf("err=%v got=%d want=%d",tc.err,rec.Code,tc.want) }
    }
}

func TestHealthProbes(t *testing.T) {
    router:=Router(&fakeFetcher{},fakePinger{err:errors.New("offline")})
    for _,tc:=range []struct{url string;want int}{
        {"/health/live",200},{"/health/ready",503},
    }{
        rec:=httptest.NewRecorder()
        router.ServeHTTP(rec,httptest.NewRequest(http.MethodGet,tc.url,nil))
        if rec.Code!=tc.want { t.Errorf("%s: got=%d want=%d",tc.url,rec.Code,tc.want) }
    }
}

 
// TestCoreRouteChineseErrors 确保错误消息中文化不影响原有 HTTP 状态码和 JSON 结构。
func TestCoreRouteChineseErrors(t *testing.T) {
    cases := []struct {
        err error
        status int
        message string
    }{
        {model.ErrInvalidRequest, http.StatusBadRequest, "环境或渠道包 ID 不合法"},
        {model.ErrNotFound, http.StatusNotFound, "渠道包不存在或未绑定该环境"},
        {model.ErrAmbiguous, http.StatusConflict, "渠道包 ID 不唯一"},
        {model.ErrInvalidConfig, http.StatusInternalServerError, "渠道包配置不合法"},
        {errors.New("sensitive database details"), http.StatusServiceUnavailable, "配置服务暂时不可用"},
    }
    for _, tc := range cases {
        recorder := httptest.NewRecorder()
        Router(&fakeFetcher{err: tc.err}, fakePinger{}).ServeHTTP(recorder,
            httptest.NewRequest(http.MethodGet, "/api/core/config/review/app", nil))
        var payload struct {
            Code int `json:"code"`
            Message string `json:"message"`
        }
        if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil { t.Fatal(err) }
        if recorder.Code != tc.status || payload.Code != tc.status || payload.Message != tc.message {
            t.Errorf("状态码或错误文案异常: HTTP=%d 响应=%+v", recorder.Code, payload)
        }
    }
}
