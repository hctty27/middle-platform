package config

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeWriter struct {
	last CreateNodeRequest
}

func (f *fakeWriter) Create(_ context.Context, input CreateNodeRequest) (Node, error) {
	f.last = input
	return Node{ID: 5, NodeType: input.NodeType, Code: input.Code, Name: input.Name, Status: *input.Status, Config: input.Config}, nil
}
func (f *fakeWriter) Update(_ context.Context, _ int64, input UpdateNodeRequest) (Node, error) {
	return Node{ID: 5, NodeType: "game", Name: input.Name, Status: *input.Status, Config: input.Config}, nil
}
func (f *fakeWriter) Delete(context.Context, int64) error { return nil }
func (f *fakeWriter) CreateSubscriber(context.Context, CreateSubscriberRequest) error { return nil }

func TestWriteRoutesDisabledByDefault(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/config/admin/nodes", strings.NewReader("{}"))
	rec := httptest.NewRecorder()
	Router(stubReader{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed { t.Fatalf("status = %d", rec.Code) }
}

func TestCreateAndDeleteWriteRoutes(t *testing.T) {
	writer := &fakeWriter{}
	handler := RouterWithWrites(stubReader{}, writer)
	req := httptest.NewRequest(http.MethodPost, "/api/config/admin/nodes", strings.NewReader(`{"nodeType":"game","code":"g1","name":"My Game","status":1,"config":"{}"}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated { t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String()) }
	var result struct { ID int64 `json:"id"` }
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil { t.Fatal(err) }
	if result.ID != 5 || writer.last.Code != "g1" { t.Fatalf("result: %+v, last: %+v", result, writer.last) }

	req = httptest.NewRequest(http.MethodDelete, "/api/config/admin/nodes/5", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent { t.Fatalf("status = %d", rec.Code) }
}

func TestCreateRejectsMalformedJSON(t *testing.T) {
	handler := RouterWithWrites(stubReader{}, &fakeWriter{})
	req := httptest.NewRequest(http.MethodPost, "/api/config/admin/nodes", strings.NewReader(`{"nodeType":"game"}{}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest { t.Fatalf("status = %d", rec.Code) }
}
