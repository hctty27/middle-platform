package store

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"strings"
	"testing"

	"hitall.com/middle-platform/sdk-service/internal/model"
)

type checkConnector struct {
	query   string
	args    []driver.NamedValue
	rows    [][]driver.Value
	columns []string
}

func (c *checkConnector) Connect(context.Context) (driver.Conn, error) { return &checkConn{c}, nil }
func (c *checkConnector) Driver() driver.Driver                        { return checkDriver{} }

type checkDriver struct{}

func (checkDriver) Open(string) (driver.Conn, error) { return nil, errors.New("not used") }

type checkConn struct{ c *checkConnector }

func (c *checkConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("not used") }
func (c *checkConn) Close() error                        { return nil }
func (c *checkConn) Begin() (driver.Tx, error)           { return nil, errors.New("not used") }
func (c *checkConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	c.c.query, c.c.args = query, args
	return &checkRows{rows: c.c.rows, columns: c.c.columns}, nil
}

type checkRows struct {
	rows    [][]driver.Value
	columns []string
}

func (r *checkRows) Columns() []string { return r.columns }
func (r *checkRows) Close() error      { return nil }
func (r *checkRows) Next(dest []driver.Value) error {
	if len(r.rows) == 0 {
		return io.EOF
	}
	copy(dest, r.rows[0])
	r.rows = r.rows[1:]
	return nil
}

func TestLookupUserCheck(t *testing.T) {
	connector := &checkConnector{
		columns: []string{"config", "code", "game_config", "global_enabled", "channel_enabled"},
		rows:    [][]driver.Value{{[]byte(`{"appSecret":"app-secret"}`), "game1", []byte(`{"gameSecret":"game-secret"}`), int64(1), int64(1)}},
	}
	db := sql.OpenDB(connector)
	defer db.Close()
	target, err := New(db).Lookup(context.Background(), "app1", "test")
	if err != nil || target.AppID != "app1" || target.Env != "test" || target.AppSecret != "app-secret" || target.GameSecret != "game-secret" {
		t.Fatalf("target=%+v err=%v", target, err)
	}
	if !strings.Contains(connector.query, "COALESCE(ch.enabled, 1)") || len(connector.args) != 4 || connector.args[0].Value != "test" || connector.args[1].Value != "test" || connector.args[2].Value != "test" || connector.args[3].Value != "app1" {
		t.Fatalf("unexpected query: %s, args=%v", connector.query, connector.args)
	}
	if _, err := New(db).Lookup(context.Background(), "app1", "bad"); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("invalid environment: %v", err)
	}
	connector.rows = nil
	if _, err := New(db).Lookup(context.Background(), "app1", "prod"); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("missing binding: %v", err)
	}
	connector.rows = [][]driver.Value{{[]byte(`{"appSecret":""}`), "game1", []byte(`{"gameSecret":"game-secret"}`), int64(1), int64(1)}}
	if _, err := New(db).Lookup(context.Background(), "app1", "test"); !errors.Is(err, model.ErrInvalidConfig) {
		t.Fatalf("missing secret: %v", err)
	}
	connector.rows = [][]driver.Value{{[]byte(`{"appSecret":""}`), "game1", []byte(`{"gameSecret":"game-secret"}`), int64(0), int64(1)}}
	if got, err := New(db).Lookup(context.Background(), "app1", "test"); err != nil || !got.SkipUserCheck {
		t.Fatalf("skip without platform secret: target=%+v err=%v", got, err)
	}
	connector.rows = [][]driver.Value{{[]byte(`{"appSecret":""}`), "game1", []byte(`{"gameSecret":""}`), int64(0), int64(1)}}
	if _, err := New(db).Lookup(context.Background(), "app1", "test"); !errors.Is(err, model.ErrInvalidConfig) {
		t.Fatalf("skip without game secret: %v", err)
	}
	for _, tc := range []struct {
		global, channel driver.Value
		skip            bool
		invalid         bool
	}{
		{int64(1), int64(1), false, false},
		{int64(0), int64(1), true, false},
		{int64(1), int64(0), true, false},
		{nil, int64(1), false, true},
		{int64(2), int64(1), false, true},
		{int64(1), int64(2), false, true},
	} {
		connector.rows = [][]driver.Value{{[]byte(`{"appSecret":"app-secret"}`), "game1", []byte(`{"gameSecret":"game-secret"}`), tc.global, tc.channel}}
		got, err := New(db).Lookup(context.Background(), "app1", "review")
		if tc.invalid {
			if !errors.Is(err, model.ErrInvalidConfig) {
				t.Fatalf("invalid switches: %v", err)
			}
		} else if err != nil || got.SkipUserCheck != tc.skip {
			t.Fatalf("switches %v/%v: target=%+v err=%v", tc.global, tc.channel, got, err)
		}
	}
}
