package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeWriter struct {
	Writer
	last CreateNodeRequest
	lastSubscriberID int64
	lastSubscriberUpdate UpdateSubscriberRequest
	lastSubscriberEnabled *bool
	deletedSubscriberID int64
	deleteErr           error
}

func (f *fakeWriter) Create(_ context.Context, input CreateNodeRequest) (Node, error) {
	f.last = input
	return Node{ID: 5, NodeType: input.NodeType, Code: input.Code, Name: input.Name, Status: *input.Status, Config: input.Config}, nil
}
func (f *fakeWriter) Update(_ context.Context, _ int64, input UpdateNodeRequest) (Node, error) {
	return Node{ID: 5, NodeType: "game", Name: input.Name, Status: *input.Status, Config: input.Config}, nil
}
func (f *fakeWriter) Delete(context.Context, int64) error                             { return f.deleteErr }
func (f *fakeWriter) CreateSubscriber(context.Context, CreateSubscriberRequest) error { return nil }
func (f *fakeWriter) UpdateSubscriber(_ context.Context, id int64, input UpdateSubscriberRequest) error {
	f.lastSubscriberID, f.lastSubscriberUpdate = id, input
	return nil
}
func (f *fakeWriter) SetSubscriberEnabled(_ context.Context, id int64, enabled bool) error {
	f.lastSubscriberID = id
	f.lastSubscriberEnabled = &enabled
	return nil
}
func (f *fakeWriter) DeleteSubscriber(_ context.Context, id int64) error {
	f.deletedSubscriberID = id
	return nil
}

// TestWriteRoutesDisabledByDefault 确保仅传入读取依赖时不注册写接口。
func TestWriteRoutesDisabledByDefault(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/config/admin/nodes", strings.NewReader("{}"))
	rec := httptest.NewRecorder()
	Router(stubReader{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d", rec.Code)
	}
}

// TestCreateAndDeleteWriteRoutes 验证写入路由能够正确调用存储层接口。
func TestCreateAndDeleteWriteRoutes(t *testing.T) {
	writer := &fakeWriter{}
	handler := RouterWithWrites(stubReader{}, writer)
	req := httptest.NewRequest(http.MethodPost, "/api/config/admin/nodes", strings.NewReader(`{"nodeType":"game","code":"g1","name":"My Game","status":1,"config":"{}"}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var result struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.ID != 5 || writer.last.Code != "g1" {
		t.Fatalf("result: %+v, last: %+v", result, writer.last)
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/config/admin/nodes/5", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d", rec.Code)
	}
}


// TestDeleteNodeReturnsSpecificBusinessError 验证删除限制返回可识别的业务错误。
func TestDeleteNodeReturnsSpecificBusinessError(t *testing.T) {
	cases := []struct {
		err     error
		code    string
		message string
	}{
		{ErrGameHasRegions, "GAME_HAS_REGIONS", ErrGameHasRegions.Error()},
		{ErrGameHasChannelApps, "GAME_HAS_CHANNEL_APPS", ErrGameHasChannelApps.Error()},
		{ErrRegionHasServerGroups, "REGION_HAS_SERVER_GROUPS", ErrRegionHasServerGroups.Error()},
		{ErrServerGroupInUse, "SERVER_GROUP_IN_USE", ErrServerGroupInUse.Error()},
	}
	for _, test := range cases {
		writer := &fakeWriter{deleteErr: test.err}
		handler := RouterWithWrites(stubReader{}, writer)
		req := httptest.NewRequest(http.MethodDelete, "/api/config/admin/nodes/5", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		var payload struct {
			Code  string `json:"code"`
			Error string `json:"error"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		if rec.Code != http.StatusConflict || payload.Code != test.code || payload.Error != test.message {
			t.Fatalf("status=%d payload=%+v, expected code=%s", rec.Code, payload, test.code)
		}
	}
}

// TestDuplicateCodeErrorResponse 验证重复编码返回明确的错误码和提示。
func TestDuplicateCodeErrorResponse(t *testing.T) {
	rec := httptest.NewRecorder()
	respond(rec, nil, ErrDuplicateCode)

	var payload struct {
		Code  string `json:"code"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusConflict || payload.Code != "DUPLICATE_CODE" || payload.Error != ErrDuplicateCode.Error() {
		t.Fatalf("status=%d payload=%+v", rec.Code, payload)
	}
}

// TestCreateRejectsMalformedJSON 验证新建节点时拒绝格式不合法的请求体。
func TestCreateRejectsMalformedJSON(t *testing.T) {
	handler := RouterWithWrites(stubReader{}, &fakeWriter{})
	req := httptest.NewRequest(http.MethodPost, "/api/config/admin/nodes", strings.NewReader(`{"nodeType":"game"}{}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestConfigSchemaRoute(t *testing.T) {
	handler := RouterWithWrites(stubReader{}, &fakeWriter{})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "/api/config/admin/config-schema/game", nil))
	if rec.Code != 200 {
		t.Fatalf("unexpected status %d: %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		NodeType string `json:"nodeType"`
		Schema   struct {
			Properties map[string]any `json:"properties"`
		} `json:"schema"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.NodeType != "game" || payload.Schema.Properties == nil {
		t.Fatalf("invalid schema: %+v", payload)
	}
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "/api/config/admin/config-schema/unknown", nil))
	if rec.Code != 404 {
		t.Fatalf("unknown type status %d", rec.Code)
	}
}


// TestSubscriberManagementRoutes 验证订阅者编辑、启停和删除路由。
func TestSubscriberManagementRoutes(t *testing.T) {
	writer := &fakeWriter{}
	handler := RouterWithWrites(stubReader{}, writer)

	req := httptest.NewRequest(http.MethodPut, "/api/config/admin/subscribers/12",
		strings.NewReader(`{"callbackUrl":"https://game.example.com/callback","secret":""}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent || writer.lastSubscriberID != 12 ||
		writer.lastSubscriberUpdate.CallbackURL != "https://game.example.com/callback" {
		t.Fatalf("update: status=%d writer=%+v body=%s", rec.Code, writer, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPatch, "/api/config/admin/subscribers/12/enabled",
		strings.NewReader(`{"enabled":false}`))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent || writer.lastSubscriberEnabled == nil || *writer.lastSubscriberEnabled {
		t.Fatalf("disable: status=%d writer=%+v body=%s", rec.Code, writer, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/config/admin/subscribers/12", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent || writer.deletedSubscriberID != 12 {
		t.Fatalf("delete: status=%d writer=%+v", rec.Code, writer)
	}

	req = httptest.NewRequest(http.MethodPatch, "/api/config/admin/subscribers/12/enabled", strings.NewReader(`{}`))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing enabled should be rejected, got %d", rec.Code)
	}
}

// TestSubscriberWriteRoutesDisabledWithoutWriter 确保只读实例不注册订阅者写接口。
func TestSubscriberWriteRoutesDisabledWithoutWriter(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		path := "/api/config/admin/subscribers"
		if method != http.MethodPost {
			path = "/api/config/admin/subscribers/12"
			if method == http.MethodPatch {
				path += "/enabled"
			}
		}
		req := httptest.NewRequest(method, path, strings.NewReader(`{}`))
		rec := httptest.NewRecorder()
		Router(stubReader{}).ServeHTTP(rec, req)
		expectedStatus := http.StatusNotFound
		if method == http.MethodPost {
			expectedStatus = http.StatusMethodNotAllowed
		}
		if rec.Code != expectedStatus {
			t.Fatalf("%s %s: expected %d, got %d", method, path, expectedStatus, rec.Code)
		}
	}
}
