package store

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"

	"hitall.com/middle-platform/config-center-access/internal/remote"
)

const nodeListSQL = "SELECT id,game_id,region_id,node_type,env_type,market_type,code,name,status FROM config_node ORDER BY id"

type marketDB struct {
	epoch    driver.Value
	revision int64
	nodes    map[int64]marketRow
}

type marketRow struct {
	id       int64
	game     driver.Value
	region   driver.Value
	nodeType string
	env      driver.Value
	market   driver.Value
	code     string
	name     string
	status   int64
}

func newMarketDB() *marketDB {
	return &marketDB{nodes: map[int64]marketRow{}}
}

type marketConnector struct{ db *marketDB }

func (c marketConnector) Connect(context.Context) (driver.Conn, error) {
	return &marketConn{db: c.db}, nil
}

func (c marketConnector) Driver() driver.Driver { return marketDriver{} }

type marketDriver struct{}

func (marketDriver) Open(string) (driver.Conn, error) { return nil, errors.New("not used") }

type marketConn struct{ db *marketDB }

func (marketConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("not used") }
func (marketConn) Close() error                        { return nil }
func (marketConn) Begin() (driver.Tx, error)           { return marketTx{}, nil }
func (marketConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return marketTx{}, nil
}

type marketTx struct{}

func (marketTx) Commit() error   { return nil }
func (marketTx) Rollback() error { return nil }

func (c *marketConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	switch query {
	case "SELECT epoch,revision FROM config_sync_state WHERE id=1 FOR UPDATE":
		return &memRows{cols: []string{"epoch", "revision"}, vals: [][]driver.Value{{c.db.epoch, c.db.revision}}}, nil
	case nodeListSQL:
		ids := make([]int64, 0, len(c.db.nodes))
		for id := range c.db.nodes {
			ids = append(ids, id)
		}
		slices.Sort(ids)
		vals := make([][]driver.Value, 0, len(ids))
		for _, id := range ids {
			row := c.db.nodes[id]
			vals = append(vals, []driver.Value{row.id, row.game, row.region, row.nodeType, row.env, row.market, row.code, row.name, row.status})
		}
		return &memRows{cols: []string{"id", "game_id", "region_id", "node_type", "env_type", "market_type", "code", "name", "status"}, vals: vals}, nil
	default:
		return nil, errors.New("unexpected query: " + query)
	}
}

func (c *marketConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	switch {
	case query == "DELETE FROM config_node":
		c.db.nodes = map[int64]marketRow{}
		return memResult{int64(len(c.db.nodes))}, nil
	case strings.HasPrefix(query, "DELETE FROM config_node WHERE"):
		id, ok := args[0].Value.(int64)
		if !ok {
			return nil, errors.New("delete id type")
		}
		delete(c.db.nodes, id)
		return memResult{1}, nil
	case strings.Contains(query, "INSERT INTO config_node"):
		if !strings.Contains(query, "market_type") || len(args) != 13 {
			return nil, errors.New("insert does not persist market_type")
		}
		id, ok := args[0].Value.(int64)
		nodeType, okType := args[3].Value.(string)
		code, okCode := args[6].Value.(string)
		name, okName := args[7].Value.(string)
		status, okStatus := args[8].Value.(int64)
		if !ok || !okType || !okCode || !okName || !okStatus {
			return nil, errors.New("insert arg type")
		}
		c.db.nodes[id] = marketRow{
			id: id, game: args[1].Value, region: args[2].Value, nodeType: nodeType,
			env: args[4].Value, market: args[5].Value, code: code, name: name, status: status,
		}
		return memResult{1}, nil
	case strings.Contains(query, "UPDATE config_sync_state"):
		revision, ok := args[1].Value.(int64)
		if !ok {
			return nil, errors.New("revision type")
		}
		c.db.epoch = args[0].Value
		c.db.revision = revision
		return memResult{1}, nil
	default:
		return nil, errors.New("unexpected exec: " + query)
	}
}

type memResult struct{ n int64 }

func (r memResult) LastInsertId() (int64, error) { return 0, nil }
func (r memResult) RowsAffected() (int64, error) { return r.n, nil }

type memRows struct {
	cols []string
	vals [][]driver.Value
	i    int
}

func (r *memRows) Columns() []string { return r.cols }
func (r *memRows) Close() error      { return nil }
func (r *memRows) Next(dest []driver.Value) error {
	if r.i >= len(r.vals) {
		return io.EOF
	}
	copy(dest, r.vals[r.i])
	r.i++
	return nil
}

func TestApplySnapshotKeepsMarketType(t *testing.T) {
	db := sql.OpenDB(marketConnector{db: newMarketDB()})
	t.Cleanup(func() { _ = db.Close() })
	replica := New(db)
	ctx := context.Background()
	now := "2026-11-16T04:05:06Z"
	gameID := int64(1)
	regionID := int64(3)
	env := "prod"
	nodes := []remote.Node{
		{ID: 1, NodeType: "game", MarketType: strPtr("domestic"), Code: "g-cn", Name: "国内", Status: 1, Config: "{}", CreatedAt: now, UpdatedAt: now},
		{ID: 2, NodeType: "game", MarketType: strPtr("overseas"), Code: "g-os", Name: "海外", Status: 1, Config: "{}", CreatedAt: now, UpdatedAt: now},
		{ID: 3, NodeType: "region", EnvType: &env, GameID: &gameID, Code: "r", Name: "大区", Status: 1, Config: "{}", CreatedAt: now, UpdatedAt: now},
		{ID: 4, NodeType: "server_group", GameID: &gameID, RegionID: &regionID, Code: "s", Name: "区服组", Status: 1, Config: "{}", CreatedAt: now, UpdatedAt: now},
	}
	applied, err := replica.Apply(ctx, State{}, State{Epoch: "epoch-1", Revision: 4}, nodes, nil, true)
	if err != nil || !applied {
		t.Fatalf("apply snapshot: applied=%v err=%v", applied, err)
	}
	assertMarkets(t, replica, map[int64]string{1: "domestic", 2: "overseas", 3: "", 4: ""})

	changed := nodes[0]
	changed.MarketType = strPtr("overseas")
	applied, err = replica.Apply(ctx, State{Epoch: "epoch-1", Revision: 4}, State{Epoch: "epoch-1", Revision: 5}, []remote.Node{changed}, nil, false)
	if err != nil || !applied {
		t.Fatalf("apply change: applied=%v err=%v", applied, err)
	}
	assertMarkets(t, replica, map[int64]string{1: "overseas", 2: "overseas", 3: "", 4: ""})

	bad := nodes[2]
	bad.MarketType = strPtr("domestic")
	if _, err := replica.Apply(ctx, State{Epoch: "epoch-1", Revision: 5}, State{Epoch: "epoch-1", Revision: 6}, []remote.Node{bad}, nil, false); err == nil {
		t.Fatal("accepted market type on a region")
	}
	assertMarkets(t, replica, map[int64]string{1: "overseas", 2: "overseas", 3: "", 4: ""})

	onlyOverseas := []remote.Node{nodes[1]}
	applied, err = replica.Apply(ctx, State{Epoch: "epoch-1", Revision: 5}, State{Epoch: "epoch-1", Revision: 6}, onlyOverseas, nil, true)
	if err != nil || !applied {
		t.Fatalf("replace snapshot: applied=%v err=%v", applied, err)
	}
	assertMarkets(t, replica, map[int64]string{2: "overseas"})
}

func assertMarkets(t *testing.T, replica *Replica, want map[int64]string) {
	t.Helper()
	got, err := replica.Nodes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) {
		t.Fatalf("nodes = %+v", got)
	}
	body, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var decoded []map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatal(err)
	}
	for _, node := range decoded {
		id := int64(node["id"].(float64))
		market, _ := node["market_type"].(string)
		if market != want[id] {
			t.Fatalf("node %d market_type = %#v, want %q; body %s", id, node["market_type"], want[id], body)
		}
		if want[id] == "" && node["market_type"] != nil {
			t.Fatalf("node %d market_type = %#v, want null", id, node["market_type"])
		}
	}
}

func strPtr(v string) *string { return &v }
