package store

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"

	"hitall.com/middle-platform/sdk-service/internal/model"
)

func TestLookupOutboundRoute(t *testing.T) {
	for _, serviceCode := range []string{"event_report", "subscribe_message"} {
		t.Run(serviceCode, func(t *testing.T) { testLookupOutboundRoute(t, serviceCode) })
	}
}

func testLookupOutboundRoute(t *testing.T, serviceCode string) {
	t.Helper()
	const appConfig = `{"appSecret":"app-secret"}`
	const gameConfig = `{"gameSecret":"game-secret","serviceAdapters":123}`
	for _, tc := range []struct {
		name string
		env  string
		row  []driver.Value
		want error
	}{
		{"ignores unrelated adapter configuration", "test", []driver.Value{appConfig, gameConfig, int64(1), int64(1)}, nil},
		{"missing environment binding", "prod", nil, model.ErrNotFound},
		{"invalid environment", "invalid", nil, model.ErrNotFound},
		{"global switch off", "review", []driver.Value{appConfig, gameConfig, int64(0), int64(1)}, model.ErrDisabled},
		{"channel switch off", "review", []driver.Value{appConfig, gameConfig, int64(1), int64(0)}, model.ErrDisabled},
		{"missing app secret", "test", []driver.Value{`{}`, gameConfig, int64(1), int64(1)}, model.ErrInvalidConfig},
		{"blank game secret", "test", []driver.Value{appConfig, `{"gameSecret":" "}`, int64(1), int64(1)}, model.ErrInvalidConfig},
		{"invalid game secret type", "test", []driver.Value{appConfig, `{"gameSecret":123}`, int64(1), int64(1)}, model.ErrInvalidConfig},
	} {
		t.Run(tc.name, func(t *testing.T) {
			connector := &checkConnector{columns: []string{"config", "game_config", "global_enabled", "channel_enabled"}}
			if tc.row != nil {
				connector.rows = [][]driver.Value{tc.row}
			}
			db := sql.OpenDB(connector)
			defer db.Close()

			got, err := New(db).LookupService(context.Background(), "app1", tc.env, serviceCode)
			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
			if tc.want == nil && (got.AppID != "app1" || got.Env != tc.env || got.AppSecret != "app-secret" || got.GameSecret != "game-secret" || got.AdapterName != "" || got.RoleQueryURL != "") {
				t.Fatal("unexpected outbound routing fields")
			}
			if tc.env == "invalid" {
				if connector.query != "" {
					t.Fatal("invalid environment reached the database")
				}
				return
			}
			if connector.query != outboundRouteQuery || strings.Contains(connector.query, "rs.config") || strings.Contains(connector.query, "rs.node_id") {
				t.Fatalf("expected outbound query without region config, got %s", connector.query)
			}
			for _, condition := range []string{"sg.game_id=a.game_id", "r.game_id=a.game_id", "r.env_type=?", "ch.env=?", "COALESCE(sv.enabled, 0)", "COALESCE(ch.enabled, 0)"} {
				if !strings.Contains(connector.query, condition) {
					t.Errorf("missing SQL condition %q", condition)
				}
			}
			wantArgs := []string{tc.env, tc.env, serviceCode, tc.env, "app1"}
			if len(connector.args) != len(wantArgs) {
				t.Fatalf("query args = %v, want %v", connector.args, wantArgs)
			}
			for i, want := range wantArgs {
				if connector.args[i].Value != want {
					t.Errorf("query arg %d = %v, want %q", i, connector.args[i].Value, want)
				}
			}
		})
	}
}

func TestLookupRoleQueryKeepsGenericRoute(t *testing.T) {
	const callback = "https://example.com/roles"
	connector := &checkConnector{
		columns: []string{"config", "code", "game_config", "region_config", "global_enabled", "channel_enabled"},
		rows:    [][]driver.Value{{`{"appSecret":"app-secret"}`, "game1", `{"gameSecret":"game-secret"}`, `{"roleQueryUrl":"` + callback + `"}`, int64(1), int64(1)}},
	}
	db := sql.OpenDB(connector)
	defer db.Close()
	got, err := New(db).LookupService(context.Background(), "app1", "test", "role_query")
	if err != nil || got.GameCode != "game1" || got.RoleQueryURL != callback || connector.query != routeQuery {
		t.Fatalf("expected the original role lookup, got error %v and query %s", err, connector.query)
	}
}
