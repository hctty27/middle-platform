package store

import (
	"context"
	"crypto/hmac"
	"errors"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/hctty27/middle-platform/config-center/internal/webhook"
	"github.com/hctty27/middle-platform/config-center/internal/service"
 "github.com/hctty27/middle-platform/config-center/internal/model"
)

// TestWriteValidation 验证创建与更新请求的基础业务约束。
func TestWriteValidation(t *testing.T) {
	status := 1
	for _, n := range []CreateNodeRequest{
		{NodeType: "channel", Code: "a", Name: "A", Status: &status, Config: "{}"},
		{NodeType: "game", ParentID: 2, Code: "a", Name: "A", Status: &status, Config: "{}"},
		{NodeType: "game", Code: "a", Name: "A", Status: &status, Config: "not-json"},
		{NodeType: "game", Code: "", Name: "A", Status: &status, Config: "{}"},
	} {
		if service.ValidateCreate(n) == nil { t.Fatalf("should reject %+v", n) }
	}
	for _, raw := range []string{
		"http://example.com/callback",
		"https://not-allowed.example/callback",
		"http://user:pass@localhost/callback",
		"file:///etc/passwd",
	} {
		if webhook.ValidateCallback(raw, "localhost") == nil { t.Fatalf("should reject callback %q", raw) }
	}
}

// TestWebhookSignature 验证回调消息签名遵循约定的 HMAC 协议。
func TestWebhookSignature(t *testing.T) {
	const secret = "integration-secret"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/internal/config/callback" {
			t.Errorf("invalid webhook request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		mac := hmac.New(sha256.New, []byte(secret))
		fmt.Fprintf(mac, "%s.test-epoch.17", r.Header.Get("X-Config-Timestamp"))
		expected := mac.Sum(nil)
		actual, err := hex.DecodeString(strings.TrimPrefix(r.Header.Get("X-Config-Signature"), "sha256="))
		if err != nil || !hmac.Equal(expected, actual) {
			t.Errorf("invalid signature")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()
	d := webhook.NewDispatcher(nil, "127.0.0.1")
	err := d.Send(context.Background(), webhook.Delivery{
		URL: server.URL + "/internal/config/callback", Secret: secret, Epoch: "test-epoch", Revision: 17,
	})
	if err != nil { t.Fatal(err) }
}

// TestDatabaseWriteAndWebhookIntegration 覆盖节点写入、版本递增及回调投递的数据库集成流程。
func TestDatabaseWriteAndWebhookIntegration(t *testing.T) {
	raw := os.Getenv("TEST_CONFIG_DB_DSN")
	if raw == "" { t.Skip("TEST_CONFIG_DB_DSN not set") }
	dsn, err := mysql.ParseDSN(raw)
	if err != nil { t.Fatal(err) }
	// Absolutely never run destructive setup against an arbitrary database.
	if dsn.DBName != "config_center_go_test" {
		t.Fatalf("integration database must be config_center_go_test, got %q", dsn.DBName)
	}
	dsn.ParseTime, dsn.Loc = true, time.UTC
	db, err := sql.Open("mysql", dsn.FormatDSN())
	if err != nil { t.Fatal(err) }
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	for _, statement := range integrationSchema {
		if _, err := db.ExecContext(ctx, statement); err != nil { t.Fatalf("%s: %v", statement, err) }
	}
	defer func() {
		for _, name := range []string{"config_channel_app_binding","config_channel_app","config_subscriber_delivery_state", "config_subscriber", "config_node_change_log", "config_node", "config_revision"} {
			if _, err := db.ExecContext(context.Background(), "DROP TABLE IF EXISTS "+name); err != nil { t.Log(err) }
		}
	}()
	store := &Store{DB: db, AllowedHosts: "127.0.0.1"}
	enabled := 1
	game, err := store.Create(ctx, CreateNodeRequest{NodeType: "game", Code: "g1", Name: "Game", Status: &enabled, Config: `{"x":1}`})
	if err != nil { t.Fatal(err) }
	regionEnv := "test"
	region, err := store.Create(ctx, CreateNodeRequest{ParentID: game.ID, NodeType: "region", EnvType: &regionEnv, Code: "r1", Name: "Region", Status: &enabled, Config: "{}"})
	if err != nil { t.Fatal(err) }
    if region.EnvType == nil || *region.EnvType != "test" { t.Fatalf("region env not persisted: %+v", region) }
    filtered, err := store.Children(ctx, game.ID, 1, 20, "", "test")
    if err != nil || len(filtered.Items) != 1 || filtered.Items[0].EnvType == nil { t.Fatalf("region env filter: %+v, %v", filtered, err) }
    rejected := "prod"
    _, err = store.Update(ctx, region.ID, UpdateNodeRequest{EnvType: &rejected, Name: region.Name, Status: &enabled, Config: "{}"})
    if !errorsIs(err, ErrConflict) { t.Fatalf("changing categorized region type should conflict: %v", err) }
    group, err := store.Create(ctx, CreateNodeRequest{ParentID: region.ID, NodeType: "server_group", Code: "s1", Name: "Group", Status: &enabled, Config: "{}"})
	if err != nil { t.Fatal(err) }
	app, err := store.CreateChannelApp(ctx, model.CreateChannelAppRequest{GameID: game.ID, Code: "a1", Name: "App", Status: &enabled, Config: `{"x":1}`})
	if err != nil { t.Fatal(err) }
	binding, err := store.CreateChannelAppBinding(ctx,app.ID,model.CreateChannelAppBindingRequest{ServerGroupID: group.ID,Status:&enabled,Config:`{"x":2}`})
	if err != nil || binding.EffectiveConfig != `{"x":2}` { t.Fatalf("binding: %+v %v",binding,err) }
	// 渠道绑定存在时快照仍仅包含节点，不能暴露渠道应用配置。
	boundSnapshot, err := store.Snapshot(ctx)
	if err != nil || len(boundSnapshot.Nodes) != 3 || boundSnapshot.Revision != 3 { t.Fatalf("snapshot: %+v %v",boundSnapshot,err) }
	payload, err := json.Marshal(boundSnapshot)
	if err != nil || strings.Contains(string(payload),"channelApps") || strings.Contains(string(payload),"bindings") {
		t.Fatalf("channel data leaked into snapshot: %s %v",payload,err)
	}
	if _, err := store.CreateChannelApp(ctx, model.CreateChannelAppRequest{GameID: game.ID, Code: "a1", Name: "Duplicate", Status: &enabled, Config: "{}"}); !errorsIs(err, ErrConflict) {
		t.Fatalf("expected duplicate app conflict, got %v",err)
	}
	initial, err := store.Revision(ctx)
	if err != nil || initial.Revision != 3 { t.Fatalf("revision after 3 node creates: %+v %v",initial,err) }
	_, err = store.Update(ctx,group.ID,UpdateNodeRequest{Name:group.Name,Status:&enabled,Config:"{ }"})
	if err != nil { t.Fatal(err) }
	same, err := store.Revision(ctx)
	if err != nil || same.Revision != initial.Revision { t.Fatalf("node no-op changed revision: %+v %v",same,err) }
	_, err = store.UpdateChannelApp(ctx,app.ID,model.UpdateChannelAppRequest{Name:"Updated App",Status:&enabled,Config:`{"x":3}`})
	if err != nil { t.Fatal(err) }
	channelRevision, err := store.Revision(ctx)
	if err != nil || channelRevision != initial { t.Fatalf("channel write changed cursor: %+v vs %+v (%v)",channelRevision,initial,err) }
	if err := store.Delete(ctx, game.ID); !errorsIs(err, ErrConflict) { t.Fatalf("expected child conflict: %v",err) }
	if err := store.Delete(ctx, group.ID); !errorsIs(err, ErrConflict) { t.Fatalf("expected bound group conflict: %v",err) }
	if err := store.DeleteChannelApp(ctx,app.ID); !errorsIs(err, ErrConflict) { t.Fatalf("expected bound app conflict: %v",err) }
	if err := store.DeleteChannelAppBinding(ctx,binding.ID); err != nil { t.Fatalf("delete binding: %v",err) }
	if err := store.DeleteChannelApp(ctx,app.ID); err != nil { t.Fatalf("delete app: %v",err) }
	finalRevision, err := store.Revision(ctx)
	if err != nil || finalRevision != initial { t.Fatalf("channel delete changed cursor: %+v vs %+v (%v)",finalRevision,initial,err) }
	changeList, err := store.Changes(ctx, 0, 100)
	if err != nil || len(changeList) != 3 { t.Fatalf("unexpected change log: count=%d err=%v",len(changeList),err) }
	snap, err := store.Snapshot(ctx)
	if err != nil || snap.Revision != 3 || len(snap.Nodes) != 3 {
		t.Fatalf("snapshot: %+v %v",snap,err)
	}
	subscribers, err := store.Subscribers(ctx)
	if err != nil || len(subscribers) != 0 { t.Fatalf("initial subscribers: %+v %v", subscribers, err) }
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusAccepted) }))
	defer server.Close()
	if err := store.CreateSubscriber(ctx, CreateSubscriberRequest{Code: "access-1", CallbackURL: server.URL+"/internal/config/callback", Secret: "test-secret"}); err != nil { t.Fatal(err) }
	dispatcher := webhook.NewDispatcher(db, "127.0.0.1")
	job, err := dispatcher.Claim(ctx)
	if err != nil || job == nil { t.Fatalf("claim: %+v %v", job, err) }
	duplicate, err := dispatcher.Claim(ctx)
	if err != nil || duplicate != nil { t.Fatalf("double claim: %+v %v", duplicate, err) }
	if err := dispatcher.Send(ctx, *job); err != nil { t.Fatal(err) }
	if err := dispatcher.Delivered(ctx, *job); err != nil { t.Fatal(err) }
	duplicate, err = dispatcher.Claim(ctx)
	if err != nil || duplicate != nil { t.Fatalf("acknowledged claim: %+v %v", duplicate, err) }
	subscribers, err = store.Subscribers(ctx)
	if err != nil || len(subscribers) != 1 || subscribers[0].AcknowledgedRevision != 3 {
		t.Fatalf("acknowledgment: %+v %v", subscribers, err)
	}

	// Every concurrent writer must receive exactly one monotonically increasing revision.
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for i := range 12 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := store.Create(ctx, CreateNodeRequest{
				NodeType: "game", Code: fmt.Sprintf("concurrent-%d", i),
				Name: fmt.Sprintf("Concurrent %d", i), Status: &enabled, Config: "{}",
			})
			errs <- err
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs { if err != nil { t.Fatal(err) } }
	all, err := store.Changes(ctx, 0, 100)
	if err != nil || len(all) != 15 { t.Fatalf("concurrent change log count = %d, err = %v", len(all), err) }
	for i, event := range all {
		if event.Revision != int64(i+1) { t.Fatalf("revision gap at %d: %d", i, event.Revision) }
	}

	// Two Go instances must never claim the same pending subscriber concurrently.
	first, second := webhook.NewDispatcher(db, "127.0.0.1"), webhook.NewDispatcher(db, "127.0.0.1")
	type claimResult struct { job *webhook.Delivery; err error }
	claims := make(chan claimResult, 2)
	var claimWG sync.WaitGroup
	for _, d := range []*webhook.Dispatcher{first, second} {
		claimWG.Add(1)
		go func(d *webhook.Dispatcher) {
			defer claimWG.Done()
			job, err := d.Claim(ctx)
			claims <- claimResult{job, err}
		}(d)
	}
	claimWG.Wait()
	close(claims)
	successful := 0
	for result := range claims {
		if result.err != nil { t.Fatal(result.err) }
		if result.job != nil {
			successful++
			if err := first.Delivered(ctx, *result.job); err != nil { t.Fatal(err) }
		}
	}
	if successful != 1 { t.Fatalf("expected exactly one lease holder, got %d", successful) }
}

func errorsIs(err, target error) bool {
	return errors.Is(err, target)
}

var integrationSchema = []string{
	`DROP TABLE IF EXISTS config_channel_app_binding`,
 `DROP TABLE IF EXISTS config_channel_app`,
 `DROP TABLE IF EXISTS config_subscriber_delivery_state`,
	`DROP TABLE IF EXISTS config_subscriber`,
	`DROP TABLE IF EXISTS config_node_change_log`,
	`DROP TABLE IF EXISTS config_node`,
	`DROP TABLE IF EXISTS config_revision`,
	`CREATE TABLE config_revision (
		id TINYINT PRIMARY KEY,revision BIGINT NOT NULL,epoch CHAR(36) NOT NULL,
		CONSTRAINT chk_config_revision_singleton CHECK(id=1)
	) ENGINE=InnoDB`,
	`INSERT INTO config_revision(id,revision,epoch) VALUES(1,0,UUID())`,
	`CREATE TABLE config_node (
		id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
		game_id BIGINT NULL,region_id BIGINT NULL,
		node_type VARCHAR(20) NOT NULL,env_type VARCHAR(16) NULL,code VARCHAR(128) COLLATE utf8mb4_bin NOT NULL,
		name VARCHAR(128) NOT NULL,status TINYINT NOT NULL DEFAULT 1,config JSON NOT NULL,
		remark VARCHAR(500),created_at DATETIME(6) NOT NULL,updated_at DATETIME(6) NOT NULL,
		CONSTRAINT chk_config_node_type CHECK(node_type IN ('game','region','server_group')),
		CONSTRAINT chk_config_node_status CHECK(status IN(0,1)),
        CONSTRAINT chk_config_node_env_type CHECK(
            (node_type='region' AND (env_type IS NULL OR env_type IN ('test','review','prod')))
            OR (node_type<>'region' AND env_type IS NULL)
        ),
		CONSTRAINT chk_config_node_ancestry CHECK(
			(node_type='game' AND game_id IS NULL AND region_id IS NULL)
			OR (node_type='region' AND COALESCE(game_id,0)>0 AND region_id IS NULL)
			OR (node_type='server_group' AND COALESCE(game_id,0)>0 AND COALESCE(region_id,0)>0)
		),
		UNIQUE KEY uq_config_node_sibling(node_type,code,((COALESCE(region_id,game_id,0)))),
		KEY idx_config_node_game_type(game_id,node_type),KEY idx_config_node_region_type(region_id,node_type)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`,
	`CREATE TABLE config_channel_app (
		id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,game_id BIGINT NOT NULL,
		code VARCHAR(128) COLLATE utf8mb4_bin NOT NULL,name VARCHAR(128) NOT NULL,
		status TINYINT NOT NULL,config JSON NOT NULL,remark VARCHAR(500) NULL,
		created_at DATETIME(6) NOT NULL,updated_at DATETIME(6) NOT NULL,
		UNIQUE KEY uq_channel_app_game_code(game_id,code)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`,
	`CREATE TABLE config_channel_app_binding (
		id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,channel_app_id BIGINT NOT NULL,
		server_group_id BIGINT NOT NULL,status TINYINT NOT NULL,config JSON NOT NULL,
		created_at DATETIME(6) NOT NULL,updated_at DATETIME(6) NOT NULL,
		UNIQUE KEY uq_channel_app_group(channel_app_id,server_group_id),
		FOREIGN KEY (channel_app_id) REFERENCES config_channel_app(id) ON DELETE RESTRICT
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`,
	`CREATE TABLE config_node_change_log(
		revision BIGINT NOT NULL PRIMARY KEY,node_id BIGINT NOT NULL,operation VARCHAR(10) NOT NULL,
		created_at DATETIME(6) NOT NULL,KEY idx_change_node(node_id),
		CONSTRAINT chk_change_operation CHECK(operation IN ('UPSERT','DELETE'))
	) ENGINE=InnoDB`,
	`CREATE TABLE config_subscriber(
		id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,code VARCHAR(128) COLLATE utf8mb4_bin NOT NULL UNIQUE,
		callback_url VARCHAR(1024) NOT NULL,webhook_secret VARCHAR(255) NOT NULL,enabled TINYINT NOT NULL DEFAULT 1,
		created_at DATETIME(6) NOT NULL,updated_at DATETIME(6) NOT NULL
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	`CREATE TABLE config_subscriber_delivery_state(
		subscriber_id BIGINT NOT NULL PRIMARY KEY,acknowledged_revision BIGINT NOT NULL DEFAULT 0,
		acknowledged_epoch CHAR(36) NULL,lease_owner VARCHAR(64) NULL,lease_until DATETIME(6) NULL,
		next_attempt_at DATETIME(6) NOT NULL,failure_count INT NOT NULL DEFAULT 0,last_error VARCHAR(500) NULL,
		updated_at DATETIME(6) NOT NULL,
		FOREIGN KEY(subscriber_id) REFERENCES config_subscriber(id) ON DELETE CASCADE
	) ENGINE=InnoDB`,
}
