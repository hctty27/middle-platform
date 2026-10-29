package config

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type stubReader struct {
	Reader
}

func (stubReader) Ready(context.Context) error { return nil }
func (stubReader) Revision(context.Context) (Revision, error) {
	return Revision{Epoch: "test-epoch", Revision: 17}, nil
}
func (stubReader) Batch(_ context.Context, ids []int64) ([]Node, error) {
	if len(ids) != 2 || ids[0] != 2 || ids[1] != 3 { return nil, ErrInvalid }
	return []Node{}, nil
}

func TestRevisionContract(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/config/sync/revision", nil)
	rec := httptest.NewRecorder()
	Router(stubReader{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK { t.Fatalf("status = %d", rec.Code) }
	var got Revision
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil { t.Fatal(err) }
	if got.Epoch != "test-epoch" || got.Revision != 17 { t.Fatalf("response = %+v", got) }
}

func TestBatchGetContract(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/config/nodes/batch-get", strings.NewReader(`{"ids":[2,3]}`))
	rec := httptest.NewRecorder()
	Router(stubReader{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestNoWriteRoute(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/config/admin/nodes", strings.NewReader("{}"))
	rec := httptest.NewRecorder()
	Router(stubReader{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestRejectInvalidPaging(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/config/admin/nodes/children?parentId=0&size=1000", nil)
	rec := httptest.NewRecorder()
	Router(stubReader{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest { t.Fatalf("status = %d", rec.Code) }
}

func TestLocalDateTimeContract(t *testing.T) {
	n := Node{NodeType: "game", Config: `{"a":1}`}
	out, err := json.Marshal(n)
	if err != nil { t.Fatal(err) }
	if !strings.Contains(string(out), "\"parentId\":0") { t.Fatalf("missing parentId: %s", out) }
}
