package remote

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"hitall.com/middle-platform/shared/signature"
)

const (
	testSubscriberCode = "access-test"
	testSyncSecret     = "config-sync-secret-0123456789abcdef0123456789"
)

func TestClientSignsAllSyncRequests(t *testing.T) {
	var calls atomic.Int64
	serverHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, 0)
		if r.Body != nil {
			var err error
			body, err = io.ReadAll(r.Body)
			if err != nil {
				t.Errorf("read request body: %v", err)
			}
		}
		if r.Header.Get("X-Subscriber-Code") != testSubscriberCode ||
			!signature.VerifySubscriberHMACRequest(r, testSyncSecret, body, time.Now()) {
			t.Errorf("request signature invalid: %s %s", r.Method, r.URL.RequestURI())
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/config/sync/revision":
			_, _ = w.Write([]byte(`{"epoch":"epoch-1","revision":7}`))
		case "/api/config/sync/changes":
			_, _ = w.Write([]byte(`[{"revision":7,"nodeId":9,"operation":"upsert"}]`))
		case "/api/config/sync/snapshot":
			_, _ = w.Write([]byte(`{"epoch":"epoch-1","revision":7,"nodes":[]}`))
		case "/api/config/nodes/batch-get":
			var input struct {
				IDs []int64 `json:"ids"`
			}
			if err := json.Unmarshal(body, &input); err != nil || len(input.IDs) != 2 {
				t.Errorf("unexpected batch request body: %s", body)
			}
			_, _ = w.Write([]byte(`[]`))
		default:
			http.NotFound(w, r)
		}
	})
	server := httptest.NewServer(signature.RequestSubscriberHMACMiddleware(
		serverHandler,
		[]string{"/api/config/sync", "/api/config/nodes"},
		func(_ context.Context, code string) (string, bool, error) {
			return testSyncSecret, code == testSubscriberCode, nil
		},
	))
	defer server.Close()

	client := New(server.URL, time.Second, 3*time.Second, testSubscriberCode, testSyncSecret)
	ctx := context.Background()
	head, err := client.Head(ctx)
	if err != nil || head.Epoch != "epoch-1" || head.Revision != 7 {
		t.Fatalf("Head: result=%+v err=%v", head, err)
	}
	changes, err := client.Changes(ctx, 6)
	if err != nil || len(changes) != 1 || changes[0].NodeID != 9 {
		t.Fatalf("Changes: result=%+v err=%v", changes, err)
	}
	snapshot, err := client.Full(ctx)
	if err != nil || snapshot.Epoch != "epoch-1" || snapshot.Revision != 7 {
		t.Fatalf("Full: result=%+v err=%v", snapshot, err)
	}
	if _, err := client.Batch(ctx, []int64{9, 10}); err != nil {
		t.Fatalf("Batch: %v", err)
	}
	if got := calls.Load(); got != 4 {
		t.Fatalf("received %d signed requests, want 4", got)
	}
}

func TestClientRejectsShortSyncSecret(t *testing.T) {
	client := New("http://127.0.0.1:1", time.Second, time.Second, testSubscriberCode, "short")
	if _, err := client.Head(context.Background()); err == nil {
		t.Fatal("short subscriber secret should be rejected")
	}
}

func TestClientRejectsMissingSubscriberCode(t *testing.T) {
	client := New("http://127.0.0.1:1", time.Second, time.Second, "", testSyncSecret)
	if _, err := client.Head(context.Background()); err == nil {
		t.Fatal("missing subscriber code should be rejected")
	}
}

func TestSnapshotJSONKeepsMarketType(t *testing.T) {
	raw := []byte(`{
		"epoch":"550e8400-e29b-41d4-a716-446655440000",
		"revision":12,
		"nodes":[
			{"id":1,"nodeType":"game","envType":null,"marketType":"domestic","gameId":null,"regionId":null,"code":"game_a","name":"示例游戏","status":1,"config":"{}","remark":null,"createdAt":"2026-11-04T10:00:00Z","updatedAt":"2026-11-04T10:00:00Z","parentId":0},
			{"id":2,"nodeType":"region","envType":"prod","marketType":null,"gameId":1,"regionId":null,"code":"r","name":"大区","status":1,"config":"{}","remark":null,"createdAt":"2026-11-04T10:00:00Z","updatedAt":"2026-11-04T10:00:00Z","parentId":1},
			{"id":3,"nodeType":"server_group","envType":null,"marketType":null,"gameId":1,"regionId":2,"code":"s","name":"区服组","status":1,"config":"{}","remark":null,"createdAt":"2026-11-04T10:00:00Z","updatedAt":"2026-11-04T10:00:00Z","parentId":2},
			{"id":4,"nodeType":"game","envType":null,"marketType":"overseas","gameId":null,"regionId":null,"code":"game_b","name":"海外游戏","status":1,"config":"{}","remark":null,"createdAt":"2026-11-04T10:00:00Z","updatedAt":"2026-11-04T10:00:00Z","parentId":0}
		]
	}`)
	var snap Snapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		t.Fatal(err)
	}
	if len(snap.Nodes) != 4 {
		t.Fatalf("nodes = %d", len(snap.Nodes))
	}
	if snap.Nodes[0].MarketType == nil || *snap.Nodes[0].MarketType != "domestic" {
		t.Fatalf("domestic game = %+v", snap.Nodes[0])
	}
	if snap.Nodes[1].MarketType != nil || snap.Nodes[2].MarketType != nil {
		t.Fatalf("non-game market leaked: %+v %+v", snap.Nodes[1].MarketType, snap.Nodes[2].MarketType)
	}
	if snap.Nodes[3].MarketType == nil || *snap.Nodes[3].MarketType != "overseas" {
		t.Fatalf("overseas game = %+v", snap.Nodes[3])
	}
}
