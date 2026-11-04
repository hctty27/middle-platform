package api

import (
    "context"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "strings"
    "testing"

    "github.com/hctty27/middle-platform/config-center/internal/model"
)

type sdkServiceReader struct { stubReader }

func (sdkServiceReader) SDKServices(context.Context) ([]model.SDKService,error) {
    return []model.SDKService{{ID:1,Code:"event_report",Name:"事件上报",Enabled:true}},nil
}
func (sdkServiceReader) SDKChannelServices(_ context.Context, appID int64, env string) ([]model.SDKChannelService,error) {
    if appID!=2 || env!="test" { return nil, ErrInvalid }
    return []model.SDKChannelService{{ServiceID:1,Code:"event_report",Enabled:false,GlobalEnabled:true,Config:json.RawMessage("{}")}},nil
}

func (sdkServiceReader) SDKServiceBindings(_ context.Context,scope string,targetID int64,env string)([]model.SDKBindingService,error){
    if scope!="server_group"||targetID!=3||env!="test"{return nil,ErrInvalid}
    return []model.SDKBindingService{{ServiceID:2,Code:"server_status",Enabled:true,GlobalEnabled:true,EffectiveEnabled:true,Config:json.RawMessage("{}")}},nil
}

type sdkServiceWriter struct { fakeWriter }
func (sdkServiceWriter) UpdateSDKService(_ context.Context, id int64, enabled bool)error {
    if id!=1 || !enabled { return ErrInvalid }
    return nil
}
func (sdkServiceWriter) UpsertSDKChannelService(_ context.Context, appID, serviceID int64, env string, enabled bool, config json.RawMessage)error {
    if appID!=2 || serviceID!=1 || env!="test" || !enabled || string(config)!="{}" { return ErrInvalid }
    return nil
}

func (sdkServiceWriter) UpsertSDKServiceBinding(_ context.Context,scope string,targetID,serviceID int64,
    env string,enabled bool,config json.RawMessage)error{
    if scope!="server_group"||targetID!=3||serviceID!=2||env!="test"||!enabled||string(config)!="{}"{
        return ErrInvalid
    }
    return nil
}

func TestSDKServiceManagementRoutes(t *testing.T) {
    handler:=RouterWithWrites(sdkServiceReader{},&sdkServiceWriter{})
    tests:=[]struct{method,path,body string;status int}{
        {"GET","/api/config/admin/sdk-services","",200},
        {"GET","/api/config/admin/channel-apps/2/services?env=test","",200},
        {"GET","/api/config/admin/channel-apps/2/services?env=bad","",400},
        {"GET","/api/config/admin/server-groups/3/services?env=test","",200},
        {"GET","/api/config/admin/server-groups/3/services?env=prod","",400},
        {"PUT","/api/config/admin/sdk-services/1",`{"enabled":true}`,204},
        {"PUT","/api/config/admin/sdk-services/1",`{}`,400},
        {"PUT","/api/config/admin/channel-apps/2/services/1?env=test",`{"enabled":true,"config":{}}`,204},
        {"PUT","/api/config/admin/channel-apps/2/services/1?env=bad",`{"enabled":true,"config":{}}`,400},
        {"PUT","/api/config/admin/server-groups/3/services/2?env=test",`{"enabled":true,"config":{}}`,204},
        {"PUT","/api/config/admin/server-groups/3/services/2?env=bad",`{"enabled":true,"config":{}}`,400},
    }
    for _,tc:=range tests {
        rec:=httptest.NewRecorder()
        req:=httptest.NewRequest(tc.method,tc.path,strings.NewReader(tc.body))
        handler.ServeHTTP(rec,req)
        if rec.Code!=tc.status { t.Fatalf("%s %s: got %d: %s",tc.method,tc.path,rec.Code,rec.Body.String()) }
    }
}

func TestSDKServiceWritesDisabledWithoutWriter(t *testing.T) {
    req:=httptest.NewRequest(http.MethodPut,"/api/config/admin/sdk-services/1",strings.NewReader(`{"enabled":true}`))
    rec:=httptest.NewRecorder()
    Router(sdkServiceReader{}).ServeHTTP(rec,req)
    if rec.Code!=http.StatusNotFound { t.Fatalf("status %d",rec.Code) }
}
