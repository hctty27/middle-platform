package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"hitall.com/middle-platform/config-center/internal/model"
)

type timeReader struct {
	Reader
	node Node
}

func (r timeReader) Snapshot(context.Context) (Snapshot, error) {
	return Snapshot{Epoch: "epoch-1", Revision: 7, Nodes: []Node{r.node}}, nil
}
func (r timeReader) Batch(context.Context, []int64) ([]Node, error) { return []Node{r.node}, nil }
func (r timeReader) Changes(context.Context, int64, int) ([]ChangeEvent, error) {
	return []ChangeEvent{{Revision: 7, NodeID: r.node.ID, Operation: "UPSERT", CreatedAt: r.node.UpdatedAt}}, nil
}

func TestSyncHTTPTimeUTC(t *testing.T) {
	stamp := model.LocalDateTime(time.Date(2026, 11, 16, 2, 5, 6, 123456000, time.FixedZone("east", 8*3600)))
	handler := Router(timeReader{node: Node{ID: 9, NodeType: "game", Code: "g", Name: "Game", Config: "{}", CreatedAt: stamp, UpdatedAt: stamp}})
	for _, tc := range []struct {
		method, path, body string
		count              int
	}{
		{http.MethodGet, "/api/config/sync/snapshot", "", 2},
		{http.MethodGet, "/api/config/sync/changes?afterRevision=6", "", 1},
		{http.MethodPost, "/api/config/nodes/batch-get", `{"ids":[9]}`, 2},
	} {
		t.Run(tc.path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body)))
			if recorder.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			if !json.Valid(recorder.Body.Bytes()) || strings.Count(recorder.Body.String(), `"2026-11-15T18:05:06.123456Z"`) != tc.count {
				t.Fatalf("unexpected sync times: %s", recorder.Body.String())
			}
		})
	}
}
