package api

import (
    "context"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "testing"

    "github.com/hctty27/middle-platform/config-center/internal/model"
)

type groupBindingsReader struct { Reader }

func (groupBindingsReader) ServerGroupBindings(_ context.Context, groupID int64) ([]model.ServerGroupBinding,error) {
    if groupID!=88 { return nil,ErrInvalid }
    return []model.ServerGroupBinding{{ChannelAppID:3,ServerGroupID:88,Status:1,
        Config:`{"login":true}`,ChannelAppName:"微信应用",ChannelAppCode:"wx_app"}},nil
}
func TestServerGroupBindingsRoute(t *testing.T) {
    req:=httptest.NewRequest(http.MethodGet,"/api/config/admin/server-groups/88/channel-apps",nil)
    rec:=httptest.NewRecorder()
    Router(groupBindingsReader{}).ServeHTTP(rec,req)
    if rec.Code!=http.StatusOK { t.Fatalf("unexpected status %d",rec.Code) }
    var result []map[string]any
    if err:=json.Unmarshal(rec.Body.Bytes(),&result);err!=nil { t.Fatal(err) }
    if len(result)!=1 || result[0]["channelAppName"]!="微信应用" || result[0]["config"]!=`{"login":true}` {
        t.Fatalf("unexpected response %+v",result)
    }
}
