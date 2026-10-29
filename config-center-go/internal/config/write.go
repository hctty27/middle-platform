package config

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"

	"github.com/go-sql-driver/mysql"
	"github.com/hctty27/middle-platform/config-center-go/internal/sqlcgen"
)

var ErrConflict = errors.New("conflicting configuration")

type CreateNodeRequest struct {
	ParentID int64   `json:"parentId"`
	NodeType string  `json:"nodeType"`
	Code     string  `json:"code"`
	Name     string  `json:"name"`
	Status   *int    `json:"status"`
	Config   string  `json:"config"`
	Remark   *string `json:"remark"`
}

type UpdateNodeRequest struct {
	Name   string  `json:"name"`
	Status *int    `json:"status"`
	Config string  `json:"config"`
	Remark *string `json:"remark"`
}

type CreateSubscriberRequest struct {
	Code        string `json:"code"`
	CallbackURL string `json:"callbackUrl"`
	Secret      string `json:"secret"`
}

func validateNode(name string, status *int, config string, remark *string) error {
	if strings.TrimSpace(name) == "" || len([]rune(name)) > 128 ||
		status == nil || (*status != 0 && *status != 1) ||
		strings.TrimSpace(config) == "" || !json.Valid([]byte(config)) {
		return ErrInvalid
	}
	if remark != nil && len([]rune(*remark)) > 500 { return ErrInvalid }
	return nil
}

func validateCreate(n CreateNodeRequest) error {
	if err := validateNode(n.Name, n.Status, n.Config, n.Remark); err != nil { return err }
	if strings.TrimSpace(n.Code) == "" || len([]rune(n.Code)) > 128 { return ErrInvalid }
	switch n.NodeType {
	case "game":
		if n.ParentID != 0 { return ErrInvalid }
	case "region", "server_group", "channel_app":
		if n.ParentID <= 0 { return ErrInvalid }
	default:
		return ErrInvalid
	}
	return nil
}

func databaseError(err error) error {
	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) {
		if mysqlErr.Number == 1062 { return fmt.Errorf("%w: duplicate sibling code", ErrConflict) }
		if mysqlErr.Number == 3819 || mysqlErr.Number == 1451 { return fmt.Errorf("%w: database constraint", ErrConflict) }
	}
	return err
}

func recordChange(ctx context.Context, tx *sql.Tx, nodeID int64, operation string) error {
	queries := sqlcgen.New(tx)
	if err := queries.IncrementRevision(ctx); err != nil { return err }
	current, err := queries.GetRevision(ctx)
	if err != nil { return err }
	_, err = tx.ExecContext(ctx, "INSERT INTO config_change_log(revision,node_id,operation,created_at) VALUES(?,?,?,UTC_TIMESTAMP(6))", current.Revision, nodeID, operation)
	return err
}

func lockRevision(ctx context.Context, tx *sql.Tx) error {
	_, err := sqlcgen.New(tx).LockRevision(ctx)
	return err
}

func (s *Store) Create(ctx context.Context, input CreateNodeRequest) (Node, error) {
	if err := validateCreate(input); err != nil { return Node{}, err }
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil { return Node{}, err }
	defer tx.Rollback()
	if err := lockRevision(ctx, tx); err != nil { return Node{}, err }

	var gameID, regionID, groupID *int64
	if input.NodeType != "game" {
		parent, err := scanNode(tx.QueryRowContext(ctx, "SELECT "+nodeColumns+" FROM config_node WHERE id=?", input.ParentID))
		if errors.Is(err, sql.ErrNoRows) { return Node{}, ErrInvalid }
		if err != nil { return Node{}, err }
		switch input.NodeType {
		case "region":
			if parent.NodeType != "game" { return Node{}, ErrInvalid }
			gameID = &parent.ID
		case "server_group":
			if parent.NodeType != "region" { return Node{}, ErrInvalid }
			gameID, regionID = parent.GameID, &parent.ID
		case "channel_app":
			if parent.NodeType != "server_group" { return Node{}, ErrInvalid }
			gameID, regionID, groupID = parent.GameID, parent.RegionID, &parent.ID
		}
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO config_node(game_id,region_id,server_group_id,node_type,code,name,status,config,remark,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))`,
		gameID, regionID, groupID, input.NodeType, input.Code, input.Name, *input.Status, input.Config, input.Remark)
	if err != nil { return Node{}, databaseError(err) }
	id, err := result.LastInsertId()
	if err != nil { return Node{}, err }
	if err := recordChange(ctx, tx, id, "UPSERT"); err != nil { return Node{}, err }
	node, err := scanNode(tx.QueryRowContext(ctx, "SELECT "+nodeColumns+" FROM config_node WHERE id=?", id))
	if err != nil { return Node{}, err }
	if err := tx.Commit(); err != nil { return Node{}, err }
	return node, nil
}

func (s *Store) Update(ctx context.Context, id int64, input UpdateNodeRequest) (Node, error) {
	if id <= 0 { return Node{}, ErrInvalid }
	if err := validateNode(input.Name, input.Status, input.Config, input.Remark); err != nil { return Node{}, err }
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil { return Node{}, err }
	defer tx.Rollback()
	if err := lockRevision(ctx, tx); err != nil { return Node{}, err }
	current, err := scanNode(tx.QueryRowContext(ctx, "SELECT "+nodeColumns+" FROM config_node WHERE id=?", id))
	if err != nil { return Node{}, err }
	var unchanged sql.NullBool
	if err := tx.QueryRowContext(ctx, `SELECT (name <=> ?) AND (status <=> ?) AND (remark <=> ?)
		AND (config = CAST(? AS JSON)) FROM config_node WHERE id=?`,
		input.Name, *input.Status, input.Remark, input.Config, id).Scan(&unchanged); err != nil {
		return Node{}, err
	}
	if !unchanged.Valid || !unchanged.Bool {
		_, err := tx.ExecContext(ctx, `UPDATE config_node SET name=?,status=?,config=?,remark=?,updated_at=UTC_TIMESTAMP(6) WHERE id=?`,
			input.Name, *input.Status, input.Config, input.Remark, id)
		if err != nil { return Node{}, databaseError(err) }
		if err := recordChange(ctx, tx, id, "UPSERT"); err != nil { return Node{}, err }
		current, err = scanNode(tx.QueryRowContext(ctx, "SELECT "+nodeColumns+" FROM config_node WHERE id=?", id))
		if err != nil { return Node{}, err }
	}
	if err := tx.Commit(); err != nil { return Node{}, err }
	return current, nil
}

func (s *Store) Delete(ctx context.Context, id int64) error {
	if id <= 0 { return ErrInvalid }
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil { return err }
	defer tx.Rollback()
	if err := lockRevision(ctx, tx); err != nil { return err }
	current, err := scanNode(tx.QueryRowContext(ctx, "SELECT "+nodeColumns+" FROM config_node WHERE id=?", id))
	if err != nil { return err }
	var childCondition string
	switch current.NodeType {
	case "game":
		childCondition = "node_type='region' AND game_id=?"
	case "region":
		childCondition = "node_type='server_group' AND region_id=?"
	case "server_group":
		childCondition = "node_type='channel_app' AND server_group_id=?"
	case "channel_app":
		childCondition = "1=0"
	default:
		return ErrInvalid
	}
	var count int64
	if current.NodeType != "channel_app" {
		if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM config_node WHERE "+childCondition, id).Scan(&count); err != nil {
			return err
		}
		if count > 0 { return ErrConflict }
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM config_node WHERE id=?", id); err != nil { return databaseError(err) }
	if err := recordChange(ctx, tx, id, "DELETE"); err != nil { return err }
	return tx.Commit()
}

func validateCallback(raw, allowed string) error {
	if len(raw) > 1024 || strings.TrimSpace(raw) == "" { return ErrInvalid }
	u, err := url.Parse(raw)
	if err != nil || u == nil || u.Hostname() == "" || u.User != nil || u.Fragment != "" { return ErrInvalid }
	host := strings.ToLower(u.Hostname())
	if !webhookHostAllowed(host, allowed) { return ErrInvalid }
	if u.Scheme != "https" && !(u.Scheme == "http" && (host == "localhost" || net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback())) {
		return ErrInvalid
	}
	return nil
}

func (s *Store) CreateSubscriber(ctx context.Context, input CreateSubscriberRequest) error {
	if strings.TrimSpace(input.Code) == "" || len([]rune(input.Code)) > 128 ||
		strings.TrimSpace(input.Secret) == "" || len(input.Secret) > 255 ||
		validateCallback(input.CallbackURL, s.webhookAllowedHosts()) != nil {
		return ErrInvalid
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil { return err }
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `INSERT INTO config_subscriber(code,callback_url,webhook_secret,enabled,created_at,updated_at)
		VALUES(?,?,?,1,UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))`, input.Code, input.CallbackURL, input.Secret)
	if err != nil { return databaseError(err) }
	id, err := result.LastInsertId()
	if err != nil { return err }
	_, err = tx.ExecContext(ctx, `INSERT INTO config_delivery_state(subscriber_id,next_attempt_at,updated_at)
		VALUES(?,UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))`, id)
	if err != nil { return err }
	return tx.Commit()
}

func (s *Store) webhookAllowedHosts() string {
	if s.AllowedHosts != "" { return s.AllowedHosts }
	return "localhost,127.0.0.1"
}
