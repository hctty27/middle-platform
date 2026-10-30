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
    Router(f,fakePinger{}).ServeHTTP(rec,httptest.NewRequest(http.MethodGet,"/api/core/prod/app",nil))
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
            httptest.NewRequest(http.MethodGet,"/api/core/review/app",nil))
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
