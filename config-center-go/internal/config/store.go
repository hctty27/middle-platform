package config

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

const nodeColumns = "id,node_type,game_id,region_id,server_group_id,code,name,status,config,remark,created_at,updated_at"

type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type Store struct {
	DB *sql.DB
	AllowedHosts string
}

func (s *Store) Ready(ctx context.Context) error { return s.DB.PingContext(ctx) }

func scanNode(scanner interface{ Scan(...any) error }) (Node, error) {
	var n Node
	var game, region, group sql.NullInt64
	var remark sql.NullString
	var created, updated time.Time
	err := scanner.Scan(&n.ID, &n.NodeType, &game, &region, &group, &n.Code, &n.Name, &n.Status, &n.Config, &remark, &created, &updated)
	if err != nil {
		return Node{}, err
	}
	if game.Valid { n.GameID = &game.Int64 }
	if region.Valid { n.RegionID = &region.Int64 }
	if group.Valid { n.ServerGroupID = &group.Int64 }
	if remark.Valid { n.Remark = &remark.String }
	n.CreatedAt, n.UpdatedAt = LocalDateTime(created), LocalDateTime(updated)
	n.ParentID = n.Parent()
	return n, nil
}

func nodes(ctx context.Context, db queryer, statement string, args ...any) ([]Node, error) {
	rows, err := db.QueryContext(ctx, statement, args...)
	if err != nil { return nil, err }
	defer rows.Close()
	result := make([]Node, 0)
	for rows.Next() {
		n, err := scanNode(rows)
		if err != nil { return nil, err }
		result = append(result, n)
	}
	return result, rows.Err()
}

func (s *Store) Nodes(ctx context.Context) ([]Node, error) {
	return nodes(ctx, s.DB, "SELECT "+nodeColumns+" FROM config_node ORDER BY id")
}

func (s *Store) Node(ctx context.Context, id int64) (Node, error) {
	if id <= 0 { return Node{}, ErrInvalid }
	return scanNode(s.DB.QueryRowContext(ctx, "SELECT "+nodeColumns+" FROM config_node WHERE id=?", id))
}

func (s *Store) Batch(ctx context.Context, ids []int64) ([]Node, error) {
	if len(ids) > 500 { return nil, ErrInvalid }
	if len(ids) == 0 { return []Node{}, nil }
	args := make([]any, len(ids))
	for i, id := range ids {
		if id <= 0 { return nil, ErrInvalid }
		args[i] = id
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	return nodes(ctx, s.DB, "SELECT "+nodeColumns+" FROM config_node WHERE id IN ("+placeholders+") ORDER BY id", args...)
}

func (s *Store) scope(ctx context.Context, parentID int64) (string, []any, error) {
	if parentID < 0 { return "", nil, ErrInvalid }
	if parentID == 0 { return "n.node_type='game'", nil, nil }
	var kind string
	if err := s.DB.QueryRowContext(ctx, "SELECT node_type FROM config_node WHERE id=?", parentID).Scan(&kind); err != nil {
		return "", nil, err
	}
	switch kind {
	case "game":
		return "n.node_type='region' AND n.game_id=?", []any{parentID}, nil
	case "region":
		return "n.node_type='server_group' AND n.region_id=?", []any{parentID}, nil
	case "server_group":
		return "n.node_type='channel_app' AND n.server_group_id=?", []any{parentID}, nil
	case "channel_app":
		return "1=0", nil, nil
	default:
		return "", nil, ErrInvalid
	}
}

func (s *Store) Tree(ctx context.Context, parentID int64) ([]NodeSummary, error) {
	condition, args, err := s.scope(ctx, parentID)
	if err != nil { return nil, err }
	result, err := nodes(ctx, s.DB, "SELECT "+nodeColumns+" FROM config_node n WHERE "+condition+" AND n.node_type IN ('game','region','server_group') ORDER BY n.id", args...)
	if err != nil { return nil, err }
	out := make([]NodeSummary, 0, len(result))
	for _, n := range result { out = append(out, summary(n)) }
	return out, nil
}

func (s *Store) Children(ctx context.Context, parentID int64, page, size int, keyword string) (NodePage, error) {
	if page < 1 || size < 1 || size > 100 || len([]rune(strings.TrimSpace(keyword))) > 128 {
		return NodePage{}, ErrInvalid
	}
	condition, args, err := s.scope(ctx, parentID)
	if err != nil { return NodePage{}, err }
	if word := strings.TrimSpace(keyword); word != "" {
		condition += " AND (n.name LIKE ? ESCAPE '!' OR n.code LIKE ? ESCAPE '!')"
		pattern := "%" + escaped(word) + "%"
		args = append(args, pattern, pattern)
	}
	var total int64
	if err := s.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM config_node n WHERE "+condition, args...).Scan(&total); err != nil {
		return NodePage{}, err
	}
	pageArgs := append(append([]any(nil), args...), size, int64(page-1)*int64(size))
	found, err := nodes(ctx, s.DB, "SELECT "+nodeColumns+" FROM config_node n WHERE "+condition+" ORDER BY n.id LIMIT ? OFFSET ?", pageArgs...)
	if err != nil { return NodePage{}, err }
	out := make([]NodeSummary, 0, len(found))
	for _, n := range found { out = append(out, summary(n)) }
	return NodePage{Items: out, Total: total, Page: page, Size: size}, nil
}

func escaped(s string) string {
	return strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(s)
}

func (s *Store) Revision(ctx context.Context) (Revision, error) {
	return revision(ctx, s.DB)
}

func revision(ctx context.Context, db queryer) (Revision, error) {
	var v Revision
	err := db.QueryRowContext(ctx, "SELECT epoch,revision FROM config_revision WHERE id=1").Scan(&v.Epoch, &v.Revision)
	return v, err
}

func (s *Store) Changes(ctx context.Context, after int64, limit int) ([]ChangeEvent, error) {
	if after < 0 || limit < 1 || limit > 500 { return nil, ErrInvalid }
	rows, err := s.DB.QueryContext(ctx, "SELECT revision,node_id,operation,created_at FROM config_change_log WHERE revision>? ORDER BY revision LIMIT ?", after, limit)
	if err != nil { return nil, err }
	defer rows.Close()
	out := make([]ChangeEvent, 0)
	for rows.Next() {
		var e ChangeEvent
		var created time.Time
		if err := rows.Scan(&e.Revision, &e.NodeID, &e.Operation, &created); err != nil { return nil, err }
		e.CreatedAt = LocalDateTime(created)
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) Snapshot(ctx context.Context) (Snapshot, error) {
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil { return Snapshot{}, err }
	defer tx.Rollback()
	v, err := revision(ctx, tx)
	if err != nil { return Snapshot{}, err }
	found, err := nodes(ctx, tx, "SELECT "+nodeColumns+" FROM config_node ORDER BY id")
	if err != nil { return Snapshot{}, err }
	if err := tx.Commit(); err != nil { return Snapshot{}, err }
	return Snapshot{Epoch: v.Epoch, Revision: v.Revision, Nodes: found}, nil
}

func (s *Store) Subscribers(ctx context.Context) ([]Subscriber, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT s.id,s.code,s.callback_url,s.enabled,d.acknowledged_revision,d.failure_count,d.last_error FROM config_subscriber s JOIN config_delivery_state d ON d.subscriber_id=s.id ORDER BY s.id")
	if err != nil { return nil, err }
	defer rows.Close()
	out := make([]Subscriber, 0)
	for rows.Next() {
		var sub Subscriber
		var last sql.NullString
		if err := rows.Scan(&sub.ID, &sub.Code, &sub.CallbackURL, &sub.Enabled, &sub.AcknowledgedRevision, &sub.Failures, &last); err != nil { return nil, err }
		if last.Valid { sub.LastError = &last.String }
		out = append(out, sub)
	}
	return out, rows.Err()
}

func (s *Store) Path(ctx context.Context, id int64) (NodeMatch, error) {
	n, err := s.Node(ctx, id)
	if err != nil { return NodeMatch{}, err }
	ancestorIDs := []int64{}
	for _, ancestor := range []*int64{n.GameID, n.RegionID, n.ServerGroupID} {
		if ancestor != nil { ancestorIDs = append(ancestorIDs, *ancestor) }
	}
	ancestors, err := s.Batch(ctx, ancestorIDs)
	if err != nil { return NodeMatch{}, err }
	index := map[int64]Node{n.ID: n}
	for _, ancestor := range ancestors { index[ancestor.ID] = ancestor }
	return match(n, index)
}

func match(n Node, index map[int64]Node) (NodeMatch, error) {
	path := make([]NodeSummary, 0, 4)
	for _, id := range []int64{value(n.GameID), value(n.RegionID), value(n.ServerGroupID), n.ID} {
		if id == 0 { continue }
		item, ok := index[id]
		if !ok { return NodeMatch{}, fmt.Errorf("missing ancestor %d: %w", id, errors.New("inconsistent configuration hierarchy")) }
		path = append(path, summary(item))
	}
	return NodeMatch{Node: summary(n), Path: path}, nil
}

func (s *Store) Search(ctx context.Context, keyword string, limit int) ([]NodeMatch, error) {
	word := strings.TrimSpace(keyword)
	if word == "" { return []NodeMatch{}, nil }
	if len([]rune(word)) > 128 || limit < 1 || limit > 50 { return nil, ErrInvalid }
	rows, err := s.DB.QueryContext(ctx,
		"SELECT id FROM config_node n WHERE (LOWER(n.name) LIKE LOWER(?) ESCAPE '!' OR LOWER(n.code) LIKE LOWER(?) ESCAPE '!') ORDER BY CASE WHEN LOWER(n.code)=LOWER(?) THEN 0 WHEN LOWER(n.code) LIKE LOWER(?) ESCAPE '!' THEN 1 ELSE 2 END,n.id LIMIT ?",
		"%"+escaped(word)+"%", "%"+escaped(word)+"%", word, escaped(word)+"%", limit)
	if err != nil { return nil, err }
	ids := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil { rows.Close(); return nil, err }
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil { return nil, err }
	found, err := s.Batch(ctx, ids)
	if err != nil { return nil, err }
	index := make(map[int64]Node, len(found))
	ancestorSet := map[int64]struct{}{}
	for _, n := range found {
		index[n.ID] = n
		for _, ancestor := range []*int64{n.GameID, n.RegionID, n.ServerGroupID} {
			if ancestor != nil { ancestorSet[*ancestor] = struct{}{} }
		}
	}
	ancestorIDs := make([]int64, 0, len(ancestorSet))
	for id := range ancestorSet { ancestorIDs = append(ancestorIDs, id) }
	ancestors, err := s.Batch(ctx, ancestorIDs)
	if err != nil { return nil, err }
	for _, n := range ancestors { index[n.ID] = n }
	out := make([]NodeMatch, 0, len(found))
	for _, id := range ids {
		n, ok := index[id]
		if !ok { return nil, sql.ErrNoRows }
		m, err := match(n, index)
		if err != nil { return nil, err }
		out = append(out, m)
	}
	return out, nil
}
