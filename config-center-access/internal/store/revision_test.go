package store

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"testing"
	"time"
)

type revisionConnector struct{ updated time.Time }

func (c revisionConnector) Connect(context.Context) (driver.Conn, error) {
	return revisionConn{c.updated}, nil
}
func (c revisionConnector) Driver() driver.Driver { return revisionDriver{} }

type revisionDriver struct{}

func (revisionDriver) Open(string) (driver.Conn, error) { return nil, errors.New("not used") }

type revisionConn struct{ updated time.Time }

func (revisionConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("not used") }
func (revisionConn) Close() error                        { return nil }
func (revisionConn) Begin() (driver.Tx, error)           { return nil, errors.New("not used") }
func (c revisionConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	if query != "SELECT epoch,revision,updated_at FROM config_sync_state WHERE id=1" {
		return nil, errors.New("unexpected query")
	}
	return &revisionRows{updated: c.updated}, nil
}

type revisionRows struct {
	updated time.Time
	done    bool
}

func (*revisionRows) Columns() []string { return []string{"epoch", "revision", "updated_at"} }
func (*revisionRows) Close() error      { return nil }
func (r *revisionRows) Next(dest []driver.Value) error {
	if r.done {
		return io.EOF
	}
	r.done = true
	copy(dest, []driver.Value{"epoch-1", int64(7), r.updated})
	return nil
}

func TestRevisionTimeUTC(t *testing.T) {
	stamp := time.Date(2026, 11, 16, 2, 5, 6, 123456000, time.FixedZone("east", 8*3600))
	db := sql.OpenDB(revisionConnector{stamp})
	defer db.Close()
	got, err := New(db).Revision(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got["epoch"] != "epoch-1" || got["revision"] != int64(7) || got["updated_at"] != "2026-11-15T18:05:06Z" {
		t.Fatalf("unexpected revision: %v", got)
	}
}
