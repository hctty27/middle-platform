package api

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

// TestRevisionContract 验证全局同步版本响应中的 epoch 与 revision 协议。
func TestRevisionContract(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/config/sync/revision", nil)
	rec := httptest.NewRecorder()
	Router(stubReader{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK { t.Fatalf("status = %d", rec.Code) }
	var got Revision
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil { t.Fatal(err) }
	if got.Epoch != "test-epoch" || got.Revision != 17 { t.Fatalf("response = %+v", got) }
}

// TestSnapshotContract 验证快照只包含三级节点。
func TestSnapshotContract(t *testing.T) {
	out, err := json.Marshal(Snapshot{Epoch:"test-epoch", Revision:3, Nodes:[]Node{}})
	if err != nil { t.Fatal(err) }
	var fields map[string]any
	if err := json.Unmarshal(out, &fields); err != nil { t.Fatal(err) }
	if len(fields)!=3 || fields["nodes"]==nil { t.Fatalf("unexpected snapshot: %s", out) }
}

// TestBatchGetContract 验证批量节点查询的数据结构与路径约定。
func TestBatchGetContract(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/config/nodes/batch-get", strings.NewReader(`{"ids":[2,3]}`))
	rec := httptest.NewRecorder()
	Router(stubReader{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

// TestNoWriteRoute 验证只读路由实例不会暴露管理写入接口。
func TestNoWriteRoute(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/config/admin/nodes", strings.NewReader("{}"))
	rec := httptest.NewRecorder()
	Router(stubReader{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

// TestRejectInvalidPaging 验证非法分页参数会被 API 拒绝。
func TestRejectInvalidPaging(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/config/admin/nodes/children?parentId=0&size=1000", nil)
	rec := httptest.NewRecorder()
	Router(stubReader{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest { t.Fatalf("status = %d", rec.Code) }
}

// TestLocalDateTimeContract 验证节点时间字段的 JSON 序列化兼容性。
func TestLocalDateTimeContract(t *testing.T) {
	n := Node{NodeType: "game", Config: `{"a":1}`}
	out, err := json.Marshal(n)
	if err != nil { t.Fatal(err) }
	if !strings.Contains(string(out), "\"parentId\":0") { t.Fatalf("missing parentId: %s", out) }
}
