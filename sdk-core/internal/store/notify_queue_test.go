package store

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"hitall.com/middle-platform/sdk-core/internal/model"
)

type queueCall struct {
	query string
	args  []driver.NamedValue
}

type queueStub struct {
	queries   []queueCall
	updates   []queueCall
	rows      [][]driver.Value
	queryErrs []error
	updateErr error
	commitErr error
	begins    int
	commits   int
	rollbacks int
	isolation driver.IsolationLevel
}

type queueConnector struct{ stub *queueStub }
type queueDriver struct{ stub *queueStub }

func (c queueConnector) Connect(context.Context) (driver.Conn, error) { return c.stub, nil }
func (c queueConnector) Driver() driver.Driver                        { return queueDriver{c.stub} }
func (d queueDriver) Open(string) (driver.Conn, error)                { return d.stub, nil }
func (s *queueStub) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (s *queueStub) Close() error { return nil }
func (s *queueStub) Begin() (driver.Tx, error) {
	return s.BeginTx(context.Background(), driver.TxOptions{})
}
func (s *queueStub) BeginTx(_ context.Context, opts driver.TxOptions) (driver.Tx, error) {
	s.begins++
	s.isolation = opts.Isolation
	return s, nil
}
func (s *queueStub) Commit() error   { s.commits++; return s.commitErr }
func (s *queueStub) Rollback() error { s.rollbacks++; return nil }
func (s *queueStub) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	s.queries = append(s.queries, queueCall{strings.Join(strings.Fields(query), " "), append([]driver.NamedValue(nil), args...)})
	if len(s.queryErrs) > 0 {
		err := s.queryErrs[0]
		s.queryErrs = s.queryErrs[1:]
		if err != nil {
			return nil, err
		}
	}
	return &queueRows{data: s.rows, columns: strings.Count(orderColumns, ",") + 1}, nil
}
func (s *queueStub) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	s.updates = append(s.updates, queueCall{strings.Join(strings.Fields(query), " "), append([]driver.NamedValue(nil), args...)})
	if s.updateErr != nil {
		return nil, s.updateErr
	}
	return driver.RowsAffected(1), nil
}

type queueRows struct {
	data    [][]driver.Value
	columns int
}

func (r *queueRows) Columns() []string {
	count := r.columns
	if len(r.data) > 0 {
		count = len(r.data[0])
	}
	return make([]string, count)
}
func (r *queueRows) Close() error { return nil }
func (r *queueRows) Next(values []driver.Value) error {
	if len(r.data) == 0 {
		return io.EOF
	}
	copy(values, r.data[0])
	r.data = r.data[1:]
	return nil
}

func claimFixture(lease time.Time) [][]driver.Value {
	return [][]driver.Value{{int64(42), "app", int64(7), "prod", "game-order", int64(100), "attach", "https://game.example/notify", "platform-order", lease.Add(-time.Hour), int64(1), int64(0), int64(8), lease}}
}

func openQueueStub(t *testing.T, stub *queueStub) *OrderStore {
	t.Helper()
	db := sql.OpenDB(queueConnector{stub})
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	return NewOrderStore(db)
}

func TestNotifyClaimUsesTwoStatementsAndDatabaseLeaseSnapshot(t *testing.T) {
	lease := time.Date(2026, 11, 18, 8, 30, 0, 0, time.UTC)
	for _, mode := range []string{"due", "candidate"} {
		t.Run(mode, func(t *testing.T) {
			stub := &queueStub{rows: claimFixture(lease)}
			store := openQueueStub(t, stub)
			var order *model.Order
			var err error
			if mode == "due" {
				order, err = store.ClaimDue(context.Background())
			} else {
				order, err = store.ClaimCandidate(context.Background(), model.NotifyCandidate{ID: 42, GameNotifyURL: "https://game.example/notify"})
			}
			if err != nil || order == nil {
				t.Fatalf("claim: %+v %v", order, err)
			}
			if len(stub.queries) != 1 || len(stub.updates) != 1 || stub.commits != 1 || stub.rollbacks != 0 {
				t.Fatalf("extra SQL or wrong transaction: %+v", stub)
			}
			query := stub.queries[0]
			for _, text := range []string{"game_notify_url", "notify_attempts", "DATE_ADD(UTC_TIMESTAMP(),INTERVAL 60 SECOND)", "payment_status=1", "notify_status<>1", "next_notify_at<=UTC_TIMESTAMP()", "FOR UPDATE SKIP LOCKED"} {
				if !strings.Contains(query.query, text) {
					t.Errorf("claim SQL missing %q: %s", text, query.query)
				}
			}
			if mode == "candidate" {
				if !strings.Contains(query.query, "id=?") || !strings.Contains(query.query, "BINARY game_notify_url=BINARY ?") || len(query.args) != 2 || query.args[0].Value != int64(42) {
					t.Fatalf("candidate not revalidated: %+v", query)
				}
			}
			update := stub.updates[0]
			if !strings.Contains(update.query, "notify_attempts=notify_attempts+1") || len(update.args) != 2 || update.args[0].Value != lease || update.args[1].Value != int64(42) {
				t.Fatalf("lease does not match locked database snapshot: %+v", update)
			}
			if order.NotifyAttempts != 9 || order.NextNotifyAt != lease || stub.isolation != driver.IsolationLevel(sql.LevelReadCommitted) {
				t.Fatalf("wrong token/lease/isolation: %+v isolation=%d", order, stub.isolation)
			}
		})
	}
}

func TestNotifyClaimNoRowsOrDatabaseFailureNeverReturnsWork(t *testing.T) {
	failure := errors.New("database unavailable")
	for _, mode := range []string{"empty", "query", "update", "commit"} {
		t.Run(mode, func(t *testing.T) {
			stub := &queueStub{rows: claimFixture(time.Now().UTC())}
			switch mode {
			case "empty":
				stub.rows = nil
			case "query":
				stub.queryErrs = []error{failure}
			case "update":
				stub.updateErr = failure
			case "commit":
				stub.commitErr = failure
			}
			order, err := openQueueStub(t, stub).ClaimCandidate(context.Background(), model.NotifyCandidate{ID: 42})
			if order != nil {
				t.Fatal("uncommitted work escaped transaction")
			}
			if mode == "empty" {
				if err != nil || len(stub.updates) != 0 || stub.rollbacks != 1 {
					t.Fatalf("empty claim wrote state: %+v %v", stub, err)
				}
			} else if !errors.Is(err, failure) {
				t.Fatalf("error lost: %v", err)
			}
		})
	}
}

func TestNotifyClaimRetriesDeadlocksWithoutExtraSnapshotRead(t *testing.T) {
	stub := &queueStub{rows: claimFixture(time.Now().UTC()), queryErrs: []error{&mysql.MySQLError{Number: 1213}, nil}}
	order, err := openQueueStub(t, stub).ClaimDue(context.Background())
	if err != nil || order == nil || order.NotifyAttempts != 9 || stub.begins != 2 || stub.rollbacks != 1 || len(stub.queries) != 2 || len(stub.updates) != 1 {
		t.Fatalf("bad retry: %+v order=%+v err=%v", stub, order, err)
	}
}

func TestListDueIsBoundedReadOnlyKeysetScanWithFixedHorizon(t *testing.T) {
	due := time.Date(2026, 11, 18, 8, 0, 0, 0, time.UTC)
	horizon := due.Add(time.Minute)
	stub := &queueStub{rows: [][]driver.Value{{int64(42), "https://game.example/notify", due, horizon}}}
	store := openQueueStub(t, stub)
	items, err := store.ListDue(context.Background(), nil, 128)
	if err != nil || len(items) != 1 {
		t.Fatalf("list: %+v %v", items, err)
	}
	if items[0].DueBefore != horizon || stub.begins != 0 || len(stub.updates) != 0 {
		t.Fatal("preview wrote state or lost database horizon")
	}
	stub.rows = [][]driver.Value{{int64(43), "https://other.example/notify", due, horizon.Add(time.Hour)}}
	next, err := store.ListDue(context.Background(), &items[0], 128)
	if err != nil || len(next) != 1 || next[0].DueBefore != horizon {
		t.Fatalf("horizon moved with new arrivals: %+v %v", next, err)
	}
	call := stub.queries[1]
	if strings.Contains(call.query, "FOR UPDATE") || !strings.Contains(call.query, "next_notify_at<=? AND (next_notify_at>? OR (next_notify_at=? AND id>?))") || !strings.HasSuffix(call.query, "ORDER BY next_notify_at,id LIMIT ?") {
		t.Fatalf("unbounded or locking preview: %s", call.query)
	}
	if len(call.args) != 5 || call.args[0].Value != horizon || call.args[1].Value != due || call.args[2].Value != due || call.args[3].Value != int64(42) || call.args[4].Value != int64(128) {
		t.Fatalf("wrong cursor: %+v", call.args)
	}
	for _, limit := range []int{0, -1, 129} {
		if _, err := store.ListDue(context.Background(), nil, limit); err == nil {
			t.Errorf("accepted limit %d", limit)
		}
	}
	if len(stub.queries) != 2 {
		t.Fatal("invalid batch size reached database")
	}
}
