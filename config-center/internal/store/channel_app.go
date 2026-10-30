package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/hctty27/middle-platform/config-center/internal/model"
	"github.com/hctty27/middle-platform/config-center/internal/service"
)

const appColumns = "id,game_id,code,name,status,config,remark,created_at,updated_at"
const bindingColumns = "id,channel_app_id,server_group_id,status,config,created_at,updated_at"

// scanApp 统一从数据库读取游戏级应用。
func scanApp(row interface{ Scan(...any) error }) (model.ChannelApp, error) {
	var a model.ChannelApp
	var remark sql.NullString
	var created, updated time.Time
	err := row.Scan(&a.ID, &a.GameID, &a.Code, &a.Name, &a.Status, &a.Config, &remark, &created, &updated)
	if remark.Valid { a.Remark = &remark.String }
	a.CreatedAt, a.UpdatedAt = model.LocalDateTime(created), model.LocalDateTime(updated)
	return a, err
}

func scanBinding(row interface{ Scan(...any) error }) (model.ChannelAppBinding, error) {
	var b model.ChannelAppBinding
	var created, updated time.Time
	err := row.Scan(&b.ID, &b.ChannelAppID, &b.ServerGroupID, &b.Status, &b.Config, &created, &updated)
	b.CreatedAt, b.UpdatedAt = model.LocalDateTime(created), model.LocalDateTime(updated)
	return b, err
}

func apps(ctx context.Context, db queryer, gameID int64) ([]model.ChannelApp, error) {
	statement := "SELECT "+appColumns+" FROM config_channel_app"
	var args []any
	if gameID > 0 { statement += " WHERE game_id=?"; args = append(args,gameID) }
	rows, err := db.QueryContext(ctx,statement+" ORDER BY id",args...)
	if err != nil { return nil, err }
	defer rows.Close()
	out := make([]model.ChannelApp,0)
	for rows.Next() {
		a, err := scanApp(rows)
		if err != nil { return nil,err }
		out = append(out,a)
	}
	return out, rows.Err()
}

func bindings(ctx context.Context, db queryer, appID int64) ([]model.ChannelAppBinding,error) {
	statement := "SELECT "+bindingColumns+" FROM config_channel_app_binding"
	var args []any
	if appID > 0 { statement += " WHERE channel_app_id=?"; args = append(args,appID) }
	rows,err := db.QueryContext(ctx,statement+" ORDER BY id",args...)
	if err != nil { return nil,err }
	defer rows.Close()
	out := make([]model.ChannelAppBinding,0)
	for rows.Next() {
		b,err := scanBinding(rows)
		if err != nil { return nil,err }
		out = append(out,b)
	}
	return out,rows.Err()
}

// MergeChannelConfig 使用 JSON Merge Patch：覆盖层同名键优先，null 删除公共键。
func MergeChannelConfig(common, override string) (string,error) {
	var a,b map[string]json.RawMessage
	if err := json.Unmarshal([]byte(common), &a); err != nil || a == nil { return "", model.ErrInvalid }
	if err := json.Unmarshal([]byte(override), &b); err != nil || b == nil { return "", model.ErrInvalid }
	for key,value := range b {
		if string(value) == "null" { delete(a,key) } else { a[key]=value }
	}
	out,err := json.Marshal(a)
	return string(out),err
}

func validObject(config string) bool {
	var obj map[string]json.RawMessage
	return json.Unmarshal([]byte(config),&obj)==nil && obj!=nil
}

func (s *Store) ChannelApps(ctx context.Context, gameID int64) ([]model.ChannelApp,error) {
	if gameID < 0 { return nil,ErrInvalid }
	return apps(ctx,s.DB,gameID)
}

func (s *Store) ChannelAppBindings(ctx context.Context, appID int64) ([]model.ChannelAppBinding,error) {
	if appID <= 0 { return nil,ErrInvalid }
	a,err := scanApp(s.DB.QueryRowContext(ctx,"SELECT "+appColumns+" FROM config_channel_app WHERE id=?",appID))
	if err != nil { return nil,err }
	found,err := bindings(ctx,s.DB,appID)
	if err != nil { return nil,err }
	for i := range found {
		found[i].EffectiveConfig,err = MergeChannelConfig(a.Config,found[i].Config)
		if err != nil { return nil,err }
	}
	return found,nil
}

// channelChanged 同步更新 epoch 和 revision，保证接入端重建完整渠道绑定副本。
func channelChanged(ctx context.Context, tx *sql.Tx, nodeID int64) error {
	if _,err := tx.ExecContext(ctx,"UPDATE config_revision SET epoch=UUID() WHERE id=1"); err != nil { return err }
	return recordChange(ctx,tx,nodeID,"UPSERT")
}

func validApp(name string,status *int,config string,remark *string) error {
	if service.ValidateNode(name,status,config,remark)!=nil || !validObject(config) { return ErrInvalid }
	return nil
}

func (s *Store) CreateChannelApp(ctx context.Context, input model.CreateChannelAppRequest) (model.ChannelApp,error) {
	if input.GameID<=0 || strings.TrimSpace(input.Code)=="" || len([]rune(input.Code))>128 ||
		validApp(input.Name,input.Status,input.Config,input.Remark)!=nil { return model.ChannelApp{},ErrInvalid }
	tx,err := s.DB.BeginTx(ctx,nil); if err!=nil { return model.ChannelApp{},err }; defer tx.Rollback()
	if err=lockRevision(ctx,tx); err!=nil { return model.ChannelApp{},err }
	var kind string
	if err=tx.QueryRowContext(ctx,"SELECT node_type FROM config_node WHERE id=?",input.GameID).Scan(&kind); err!=nil || kind!="game" { return model.ChannelApp{},ErrInvalid }
	res,err := tx.ExecContext(ctx,`INSERT INTO config_channel_app(game_id,code,name,status,config,remark,created_at,updated_at)
		VALUES(?,?,?,?,?,?,UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))`,input.GameID,input.Code,input.Name,*input.Status,input.Config,input.Remark)
	if err!=nil { return model.ChannelApp{},databaseError(err) }
	id,err:=res.LastInsertId(); if err!=nil { return model.ChannelApp{},err }
	if err=channelChanged(ctx,tx,input.GameID); err!=nil { return model.ChannelApp{},err }
	a,err:=scanApp(tx.QueryRowContext(ctx,"SELECT "+appColumns+" FROM config_channel_app WHERE id=?",id))
	if err!=nil { return model.ChannelApp{},err }
	return a,tx.Commit()
}

func (s *Store) UpdateChannelApp(ctx context.Context,id int64,input model.UpdateChannelAppRequest) (model.ChannelApp,error) {
	if id<=0 || validApp(input.Name,input.Status,input.Config,input.Remark)!=nil { return model.ChannelApp{},ErrInvalid }
	tx,err:=s.DB.BeginTx(ctx,nil); if err!=nil { return model.ChannelApp{},err }; defer tx.Rollback()
	if err=lockRevision(ctx,tx); err!=nil { return model.ChannelApp{},err }
	current,err:=scanApp(tx.QueryRowContext(ctx,"SELECT "+appColumns+" FROM config_channel_app WHERE id=?",id))
	if err!=nil { return model.ChannelApp{},err }
	_,err=tx.ExecContext(ctx,"UPDATE config_channel_app SET name=?,status=?,config=?,remark=?,updated_at=UTC_TIMESTAMP(6) WHERE id=?",input.Name,*input.Status,input.Config,input.Remark,id)
	if err!=nil { return model.ChannelApp{},databaseError(err) }
	if err=channelChanged(ctx,tx,current.GameID); err!=nil { return model.ChannelApp{},err }
	a,err:=scanApp(tx.QueryRowContext(ctx,"SELECT "+appColumns+" FROM config_channel_app WHERE id=?",id))
	if err!=nil { return model.ChannelApp{},err }
	return a,tx.Commit()
}

func (s *Store) DeleteChannelApp(ctx context.Context,id int64) error {
	if id<=0 { return ErrInvalid }
	tx,err:=s.DB.BeginTx(ctx,nil); if err!=nil { return err }; defer tx.Rollback()
	if err=lockRevision(ctx,tx); err!=nil { return err }
	var gameID int64
	if err=tx.QueryRowContext(ctx,"SELECT game_id FROM config_channel_app WHERE id=?",id).Scan(&gameID); err!=nil { return err }
	var count int
	if err=tx.QueryRowContext(ctx,"SELECT COUNT(*) FROM config_channel_app_binding WHERE channel_app_id=?",id).Scan(&count); err!=nil { return err }
	if count!=0 { return ErrConflict }
	if _,err=tx.ExecContext(ctx,"DELETE FROM config_channel_app WHERE id=?",id); err!=nil { return databaseError(err) }
	if err=channelChanged(ctx,tx,gameID); err!=nil { return err }
	return tx.Commit()
}

func (s *Store) CreateChannelAppBinding(ctx context.Context,appID int64,input model.CreateChannelAppBindingRequest) (model.ChannelAppBinding,error) {
	if appID<=0 || input.ServerGroupID<=0 || input.Status==nil || (*input.Status!=0 && *input.Status!=1) || !validObject(input.Config) {
		return model.ChannelAppBinding{},ErrInvalid
	}
	tx,err:=s.DB.BeginTx(ctx,nil); if err!=nil { return model.ChannelAppBinding{},err }; defer tx.Rollback()
	if err=lockRevision(ctx,tx); err!=nil { return model.ChannelAppBinding{},err }
	var gameID int64
	if err=tx.QueryRowContext(ctx,"SELECT game_id FROM config_channel_app WHERE id=?",appID).Scan(&gameID); err!=nil { return model.ChannelAppBinding{},err }
	var groupGame int64; var kind string
	if err=tx.QueryRowContext(ctx,"SELECT game_id,node_type FROM config_node WHERE id=?",input.ServerGroupID).Scan(&groupGame,&kind); err!=nil || kind!="server_group" || groupGame!=gameID { return model.ChannelAppBinding{},ErrInvalid }
	res,err:=tx.ExecContext(ctx,`INSERT INTO config_channel_app_binding(channel_app_id,server_group_id,status,config,created_at,updated_at)
		VALUES(?,?,?,?,UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))`,appID,input.ServerGroupID,*input.Status,input.Config)
	if err!=nil { return model.ChannelAppBinding{},databaseError(err) }
	id,err:=res.LastInsertId(); if err!=nil { return model.ChannelAppBinding{},err }
	if err=channelChanged(ctx,tx,input.ServerGroupID); err!=nil { return model.ChannelAppBinding{},err }
	b,err:=scanBinding(tx.QueryRowContext(ctx,"SELECT "+bindingColumns+" FROM config_channel_app_binding WHERE id=?",id))
	if err!=nil { return model.ChannelAppBinding{},err }
	a,err:=scanApp(tx.QueryRowContext(ctx,"SELECT "+appColumns+" FROM config_channel_app WHERE id=?",appID))
	if err!=nil { return model.ChannelAppBinding{},err }
	b.EffectiveConfig,err=MergeChannelConfig(a.Config,b.Config); if err!=nil { return model.ChannelAppBinding{},err }
	return b,tx.Commit()
}

func (s *Store) UpdateChannelAppBinding(ctx context.Context,id int64,input model.UpdateChannelAppBindingRequest) (model.ChannelAppBinding,error) {
	if id<=0 || input.Status==nil || (*input.Status!=0 && *input.Status!=1) || !validObject(input.Config) { return model.ChannelAppBinding{},ErrInvalid }
	tx,err:=s.DB.BeginTx(ctx,nil); if err!=nil { return model.ChannelAppBinding{},err }; defer tx.Rollback()
	if err=lockRevision(ctx,tx); err!=nil { return model.ChannelAppBinding{},err }
	current,err:=scanBinding(tx.QueryRowContext(ctx,"SELECT "+bindingColumns+" FROM config_channel_app_binding WHERE id=?",id))
	if err!=nil { return model.ChannelAppBinding{},err }
	_,err=tx.ExecContext(ctx,"UPDATE config_channel_app_binding SET status=?,config=?,updated_at=UTC_TIMESTAMP(6) WHERE id=?",*input.Status,input.Config,id)
	if err!=nil { return model.ChannelAppBinding{},databaseError(err) }
	if err=channelChanged(ctx,tx,current.ServerGroupID); err!=nil { return model.ChannelAppBinding{},err }
	b,err:=scanBinding(tx.QueryRowContext(ctx,"SELECT "+bindingColumns+" FROM config_channel_app_binding WHERE id=?",id))
	if err!=nil { return model.ChannelAppBinding{},err }
	a,err:=scanApp(tx.QueryRowContext(ctx,"SELECT "+appColumns+" FROM config_channel_app WHERE id=?",b.ChannelAppID))
	if err!=nil { return model.ChannelAppBinding{},err }
	b.EffectiveConfig,err=MergeChannelConfig(a.Config,b.Config); if err!=nil { return model.ChannelAppBinding{},err }
	return b,tx.Commit()
}

func (s *Store) DeleteChannelAppBinding(ctx context.Context,id int64) error {
	if id<=0 { return ErrInvalid }
	tx,err:=s.DB.BeginTx(ctx,nil); if err!=nil { return err }; defer tx.Rollback()
	if err=lockRevision(ctx,tx); err!=nil { return err }
	var groupID int64
	if err=tx.QueryRowContext(ctx,"SELECT server_group_id FROM config_channel_app_binding WHERE id=?",id).Scan(&groupID); err!=nil { return err }
	if _,err=tx.ExecContext(ctx,"DELETE FROM config_channel_app_binding WHERE id=?",id); err!=nil { return databaseError(err) }
	if err=channelChanged(ctx,tx,groupID); err!=nil { return err }
	return tx.Commit()
}

// 供全量快照复用事务中的一致性读取。
func snapshotChannelApps(ctx context.Context,db queryer) ([]model.ChannelApp,[]model.ChannelAppBinding,error) {
	a,err:=apps(ctx,db,0); if err!=nil { return nil,nil,err }
	b,err:=bindings(ctx,db,0); if err!=nil { return nil,nil,err }
	index:=make(map[int64]string,len(a))
	for _,item:=range a { index[item.ID]=item.Config }
	for i:=range b {
		base,ok:=index[b[i].ChannelAppID]
		if !ok { return nil,nil,errors.New("orphan channel app binding") }
		b[i].EffectiveConfig,err=MergeChannelConfig(base,b[i].Config)
		if err!=nil { return nil,nil,err }
	}
	return a,b,nil
}
