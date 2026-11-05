package api

import (
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "testing"
)

func TestSDKNodeConfigValidation(t *testing.T) {
    cases:=[]struct{kind,config string;valid bool}{
        {"game",`{"gameSecret":"secret","serviceAdapters":{"user_check":"standard"}}`,true},
        {"region",`{"payNotifyUrl":"https://example.com/pay"}`,true},
        {"server_group",`{"custom":"v"}`,true},
        {"game",`{"payNotifyUrl":"https://example.com/pay"}`,false},
        {"region",`{"gameSecret":"secret"}`,false},
        {"game",`{"serviceAdapters":[]}`,false},
        {"game",`{"serviceAdapters":null}`,false},
        {"region",`{"roleControlUrl":42}`,false},
        {"region",`[]`,false},
        {"region",`{"roleControlUrl":null}`,false},
        {"unknown",`{}`,false},
    }
    for _,tc:=range cases{
        if validSDKNodeConfig(tc.kind,tc.config)!=tc.valid{
           t.Errorf("kind=%s config=%s expected valid=%v",tc.kind,tc.config,tc.valid)
        }
    }
}

func TestSDKAdminRouteRegistration(t *testing.T) {
    mux:=http.NewServeMux()
    registerAdminRoutes(mux,nil)
    cases:=[]struct{method,path string;status int}{
        {"GET","/api/core/admin/node-configs/invalid",400},
        {"PUT","/api/core/admin/node-configs/0",400},
        {"GET","/api/core/admin/channel-apps/server-groups?envType=wrong",400},
        {"GET","/api/core/admin/channel-apps",400},
        {"GET","/api/core/admin/unknown",404},
    }
    for _,tc:=range cases{
       rec:=httptest.NewRecorder()
       req:=httptest.NewRequest(tc.method,tc.path,nil)
       mux.ServeHTTP(rec,req)
       if rec.Code!=tc.status {t.Errorf("%s %s: got %d want %d",tc.method,tc.path,rec.Code,tc.status)}
    }
}

func TestSDKConfigObjectShape(t *testing.T){
   object,valid:=jsonObject(`{"custom":{"nested":true}}`)
   if !valid || len(object)!=1 {t.Fatal("expected valid object")}
   _,valid=jsonObject(`{"a":1}{"b":2}`)
   if valid {t.Fatal("multiple JSON values must be rejected")}
   var _ json.RawMessage
}
