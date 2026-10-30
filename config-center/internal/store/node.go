package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// 节点查询统一列顺序，必须与 scanNode 的扫描字段严格保持一致。
const nodeColumns = "id,node_type,game_id,region_id,server_group_id,env_type,code,name,status,config,remark,created_at,updated_at"

// queryer 让普通连接与事务共用同一套节点读取实现。
type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// Store 管理节点数据访问、数据库连接及回调白名单。
type Store struct {
	DB *sql.DB
	AllowedHosts string
}

// Ready 测试数据库连接状态以提供就绪探针。
func (s *Store) Ready(ctx context.Context) error { return s.DB.PingContext(ctx) }

// scanNode 将数据库字段扫描成统一节点对象。
func scanNode(scanner interface{ Scan(...any) error }) (Node, error) {
	var n Node
	var game, region, group sql.NullInt64
	var remark, envType sql.NullString
	var created, updated time.Time
	err := scanner.Scan(&n.ID, &n.NodeType, &game, &region, &group, &envType, &n.Code, &n.Name, &n.Status, &n.Config, &remark, &created, &updated)
	if err != nil {
		return Node{}, err
	}
	if game.Valid { n.GameID = &game.Int64 }
	if region.Valid { n.RegionID = &region.Int64 }
	if group.Valid { n.ServerGroupID = &group.Int64 }
	if envType.Valid { n.EnvType = &envType.String }
	if remark.Valid { n.Remark = &remark.String }
	n.CreatedAt, n.UpdatedAt = LocalDateTime(created), LocalDateTime(updated)
	n.ParentID = n.Parent()
	return n, nil
}

// nodes 执行节点查询并正确关闭 SQL 结果集。
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

// Nodes 获取所有配置节点。
func (s *Store) Nodes(ctx context.Context) ([]Node, error) {
	return nodes(ctx, s.DB, "SELECT "+nodeColumns+" FROM config_node ORDER BY id")
}

// Node 根据主键查询完整节点；不存在时由上层统一转换为 HTTP 错误。
func (s *Store) Node(ctx context.Context, id int64) (Node, error) {
	if id <= 0 { return Node{}, ErrInvalid }
	return scanNode(s.DB.QueryRowContext(ctx, "SELECT "+nodeColumns+" FROM config_node WHERE id=?", id))
}

// Batch 限制批量数量并读取指定节点集合。
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

// scope 根据父节点类型构造子项的 SQL 过滤条件。
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

// Tree 返回当前配置层级需要的节点摘要。
func (s *Store) Tree(ctx context.Context, parentID int64) ([]NodeSummary, error) {
	condition, args, err := s.scope(ctx, parentID)
	if err != nil { return nil, err }
	result, err := nodes(ctx, s.DB, "SELECT "+nodeColumns+" FROM config_node n WHERE "+condition+" AND n.node_type IN ('game','region','server_group') ORDER BY n.id", args...)
	if err != nil { return nil, err }
	out := make([]NodeSummary, 0, len(result))
	for _, n := range result { out = append(out, summary(n)) }
	return out, nil
}

// Children 分页查询直接子项并支持搜索过滤。
func (s *Store) Children(ctx context.Context, parentID int64, page, size int, keyword, envType string) (NodePage, error) {
	if page < 1 || size < 1 || size > 100 || len([]rune(strings.TrimSpace(keyword))) > 128 {
		return NodePage{}, ErrInvalid
	}
	condition, args, err := s.scope(ctx, parentID)
    if err != nil { return NodePage{}, err }
    if envType != "" {
        switch envType {
        case "test", "review", "production":
        default: return NodePage{}, ErrInvalid
        }
        if parentID == 0 { return NodePage{}, ErrInvalid }
        condition += " AND n.node_type='region' AND n.env_type=?"
        args = append(args, envType)
    }
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

// escaped 转义 LIKE 模糊匹配中的特殊字符，防止用户输入被解释为通配符。
func escaped(s string) string {
	return strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(s)
}

// Path 构造指定节点从游戏节点开始的祖先路径。
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

// match 合并节点及祖先形成完整路径。
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

// Search 查找匹配节点并批量补齐祖先信息。
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
