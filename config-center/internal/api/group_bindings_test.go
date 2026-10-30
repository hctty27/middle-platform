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

func (groupBindingsReader) ServerGroupBindings(_ context.Context, groupID int64) ([]model.ServerGroupBinding, error) {
	if groupID != 88 { return nil, ErrInvalid }
	return []model.ServerGroupBinding{{
		ChannelAppBinding: model.ChannelAppBinding{ID: 12, ChannelAppID: 3, ServerGroupID: 88, Status: 1, Config: "{}",
			EffectiveConfig: `{"login":true}`},
		ChannelAppName: "微信应用", ChannelAppCode: "wx_app",
	}}, nil
}

func TestServerGroupBindingsRoute(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/config/admin/server-groups/88/channel-app-bindings", nil)
	recorder := httptest.NewRecorder()
	Router(groupBindingsReader{}).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK { t.Fatalf("unexpected status: %d", recorder.Code) }
	var result []model.ServerGroupBinding
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil { t.Fatal(err) }
	if len(result) != 1 || result[0].ChannelAppName != "微信应用" || result[0].EffectiveConfig != `{"login":true}` {
		t.Fatalf("unexpected response: %+v", result)
	}
}
