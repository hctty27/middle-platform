package store

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"time"

	"github.com/hctty27/middle-platform/config-center/internal/model"
)

func validSDKEnv(env string) bool {
	return env == "prod" || env == "test" || env == "review"
}

const sdkServiceColumns = "id,code,name,description,enabled,created_at,updated_at"

// SDKServices 返回全局服务目录，按 ID 稳定排序。
func (s *Store) SDKServices(ctx context.Context) ([]model.SDKService, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT "+sdkServiceColumns+" FROM sdk_service ORDER BY id")
	if err != nil { return nil, err }
	defer rows.Close()

	items := make([]model.SDKService, 0)
	for rows.Next() {
		var item model.SDKService
		var enabled int
		var created, updated time.Time
		if err := rows.Scan(&item.ID, &item.Code, &item.Name, &item.Description,
			&enabled, &created, &updated); err != nil { return nil, err }
		item.Enabled = enabled == 1
		item.CreatedAt = model.LocalDateTime(created)
		item.UpdatedAt = model.LocalDateTime(updated)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) UpdateSDKService(ctx context.Context, id int64, enabled bool) error {
	if id <= 0 { return ErrInvalid }
	result, err := s.DB.ExecContext(ctx,
		"UPDATE sdk_service SET enabled=?,updated_at=UTC_TIMESTAMP(3) WHERE id=?", enabled, id)
	if err != nil { return databaseError(err) }
	affected, err := result.RowsAffected()
	if err != nil { return err }
	if affected > 0 { return nil }
	var exists int
	return s.DB.QueryRowContext(ctx, "SELECT 1 FROM sdk_service WHERE id=?", id).Scan(&exists)
}

// channelAppState 检查渠道包及对应环境的游戏、区服组、大区是否启用。
func channelAppState(ctx context.Context, db queryer, appID int64, env string) (bool, error) {
	var statuses [4]sql.NullInt64
	err := db.QueryRowContext(ctx, `
SELECT app.status,g.status,sg.status,r.status FROM config_channel_app app
LEFT JOIN config_node g ON g.id=app.game_id AND g.node_type='game'
LEFT JOIN config_node sg ON sg.id=CASE ?
  WHEN 'prod' THEN app.prod_server_group_id
  WHEN 'test' THEN app.test_server_group_id
  WHEN 'review' THEN app.review_server_group_id
END AND sg.node_type='server_group' AND sg.game_id=app.game_id
LEFT JOIN config_node r ON r.id=sg.region_id AND r.node_type='region'
 AND r.game_id=app.game_id AND r.env_type=?
WHERE app.id=?`, env, env, appID).
		Scan(&statuses[0], &statuses[1], &statuses[2], &statuses[3])
	if err != nil { return false, err }
	for _, status := range statuses {
		if !status.Valid || status.Int64 != 1 { return false, nil }
	}
	return true, nil
}

// SDKChannelServices 返回指定渠道包环境的服务配置，未绑定时默认关闭。
func (s *Store) SDKChannelServices(ctx context.Context, appID int64, env string) ([]model.SDKChannelService, error) {
	if appID <= 0 || !validSDKEnv(env) { return nil, ErrInvalid }
	active, err := channelAppState(ctx, s.DB, appID, env)
	if err != nil { return nil, err }
	rows, err := s.DB.QueryContext(ctx, `
SELECT sv.id,sv.code,sv.name,sv.description,sv.enabled,
       COALESCE(ch.enabled,0),COALESCE(ch.config,JSON_OBJECT())
FROM sdk_service sv
LEFT JOIN sdk_service_channel ch ON ch.service_id=sv.id AND ch.channel_app_id=? AND ch.env=?
ORDER BY sv.id`, appID, env)
	if err != nil { return nil, err }
	defer rows.Close()

	items := make([]model.SDKChannelService, 0)
	for rows.Next() {
		var item model.SDKChannelService
		var globalEnabled, bindingEnabled int
		var raw []byte
		if err := rows.Scan(&item.ServiceID, &item.Code, &item.Name, &item.Description,
			&globalEnabled, &bindingEnabled, &raw); err != nil { return nil, err }
		item.GlobalEnabled = globalEnabled == 1
		item.Enabled = bindingEnabled == 1
		item.EffectiveEnabled = item.GlobalEnabled && item.Enabled && active
		item.Config = append(json.RawMessage(nil), raw...)
		items = append(items, item)
	}
	return items, rows.Err()
}

func validBindingConfig(config json.RawMessage) bool {
	if len(config) == 0 || len(config) > 64<<10 { return false }
	decoder := json.NewDecoder(bytes.NewReader(config))
	var object map[string]json.RawMessage
	if err := decoder.Decode(&object); err != nil || object == nil { return false }
	return decoder.Decode(new(any)) == io.EOF
}

// UpsertSDKChannelService 以配置版本锁串行化服务开关写入与渠道包删除。
func (s *Store) UpsertSDKChannelService(ctx context.Context, appID, serviceID int64,
	env string, enabled bool, config json.RawMessage) error {
	if appID <= 0 || serviceID <= 0 || !validSDKEnv(env) || !validBindingConfig(config) {
		return ErrInvalid
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil { return err }
	defer tx.Rollback()
	if err = lockRevision(ctx, tx); err != nil { return err }

	var exists int
	if err = tx.QueryRowContext(ctx, "SELECT 1 FROM sdk_service WHERE id=?", serviceID).Scan(&exists); err != nil {
		return err
	}
	if _, err = channelAppState(ctx, tx, appID, env); err != nil { return err }
	_, err = tx.ExecContext(ctx, `
INSERT INTO sdk_service_channel (service_id,channel_app_id,env,enabled,config)
VALUES (?,?,?,?,?)
ON DUPLICATE KEY UPDATE enabled=VALUES(enabled),config=VALUES(config),updated_at=UTC_TIMESTAMP(3)`,
		serviceID, appID, env, enabled, string(config))
	if err != nil { return databaseError(err) }
	return tx.Commit()
}
