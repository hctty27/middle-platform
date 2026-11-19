package store

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"
	"testing"

	"hitall.com/middle-platform/sdk-core/internal/model"
)

// No database connection is used: this driver records SQL and emulates only
// the WHERE fencing predicates, not MySQL locking or DATE_ADD semantics.
type completionStub struct {
	query   string
	args    []driver.NamedValue
	attempt int64
	status  int
	err     error
}
type completionConnector struct{ stub *completionStub }

func (c completionConnector) Connect(context.Context) (driver.Conn, error) { return c.stub, nil }
func (c completionConnector) Driver() driver.Driver                        { return completionDriver{c.stub} }

type completionDriver struct{ stub *completionStub }

func (d completionDriver) Open(string) (driver.Conn, error) { return d.stub, nil }
func (s *completionStub) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("not implemented")
}
func (s *completionStub) Close() error              { return nil }
func (s *completionStub) Begin() (driver.Tx, error) { return nil, errors.New("not implemented") }
func (s *completionStub) ExecContext(_ context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	s.query = strings.Join(strings.Fields(q), " ")
	s.args = append([]driver.NamedValue(nil), args...)
	if s.err != nil {
		return nil, s.err
	}
	if !strings.Contains(s.query, "payment_status=1 AND notify_status<>1 AND notify_attempts=?") {
		return nil, fmt.Errorf("missing fencing: %s", s.query)
	}
	if args[len(args)-1].Value != s.attempt || s.status == 1 {
		return driver.RowsAffected(0), nil
	}
	if strings.Contains(s.query, "SET notify_status=1") {
		s.status = 1
	}
	if strings.Contains(s.query, "SET notify_status=2") {
		s.status = 2
	}
	return driver.RowsAffected(1), nil
}

func TestCompleteNotifyLocalDeferralSQLAndFencing(t *testing.T) {
	stub := &completionStub{attempt: 9, status: 2}
	db := sql.OpenDB(completionConnector{stub})
	defer db.Close()
	store := NewOrderStore(db)
	if err := store.CompleteNotify(context.Background(), 42, 9, fmt.Errorf("busy: %w", model.ErrNotifyDeferred)); err != nil {
		t.Fatal(err)
	}
	setClause := strings.SplitN(stub.query, " WHERE ", 2)[0]
	if !strings.Contains(stub.query, "INTERVAL 5 SECOND") || strings.Contains(setClause, "notify_status") || strings.Contains(setClause, "notify_attempts") {
		t.Fatalf("bad local deferral SQL: %s", stub.query)
	}
	if strings.Contains(stub.query, "POW(") || strings.Contains(stub.query, "notify_attempts=notify_attempts") {
		t.Fatal("deferral changes failure backoff or attempts")
	}
	if len(stub.args) != 3 || stub.args[0].Value != model.ErrNotifyDeferred.Error() || stub.args[1].Value != int64(42) || stub.args[2].Value != int64(9) {
		t.Fatalf("bad parameters: %#v", stub.args)
	}
	if stub.status != 2 || stub.attempt != 9 {
		t.Fatal("deferral modified status/token")
	}
	// An old success cannot overwrite a newer attempt.
	if err := store.CompleteNotify(context.Background(), 42, 8, nil); !errors.Is(err, model.ErrNotifyResultNotApplied) {
		t.Fatal(err)
	}
	if stub.status != 2 {
		t.Fatal("stale attempt overwrote current result")
	}
	if err := store.CompleteNotify(context.Background(), 42, 9, nil); err != nil {
		t.Fatal(err)
	}
	if stub.status != 1 {
		t.Fatal("current success not persisted")
	}
	if err := store.CompleteNotify(context.Background(), 42, 9, model.ErrNotifyDeferred); !errors.Is(err, model.ErrNotifyResultNotApplied) {
		t.Fatal(err)
	}
	if stub.status != 1 {
		t.Fatal("deferral overwrote terminal success")
	}
}

func TestCompleteNotifyRealFailureAndSaveFailure(t *testing.T) {
	stub := &completionStub{attempt: 4}
	db := sql.OpenDB(completionConnector{stub})
	defer db.Close()
	store := NewOrderStore(db)
	if err := store.CompleteNotify(context.Background(), 42, 4, errors.New("real\nnetwork error")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stub.query, "SET notify_status=2") || !strings.Contains(stub.query, "LEAST(3600,30*POW(2,LEAST(7,notify_attempts)))") {
		t.Fatalf("real failure backoff changed: %s", stub.query)
	}
	if stub.args[0].Value != "real network error" {
		t.Fatal("bad failure summary")
	}
	stub.err = errors.New("database unavailable")
	if err := store.CompleteNotify(context.Background(), 42, 4, model.ErrNotifyDeferred); !errors.Is(err, stub.err) {
		t.Fatalf("save error not propagated: %v", err)
	}
	if stub.attempt != 4 || stub.status != 2 {
		t.Fatal("save failure changed lease state")
	}
}
