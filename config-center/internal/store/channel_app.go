package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"github.com/hctty27/middle-platform/config-center/internal/model"
	"github.com/hctty27/middle-platform/config-center/internal/service"
)

const appColumns = "id,game_id,code,name,status,config,remark,prod_server_group_id,test_server_group_id,review_server_group_id,created_at,updated_at"

// scanApp 统一从数据库读取游戏级应用。
func scanApp(row interface{ Scan(...any) error }) (model.ChannelApp, error) {
	var a model.ChannelApp
	var remark sql.NullString
    var prod, test, review sql.NullInt64
    var created, updated time.Time
    err := row.Scan(&a.ID, &a.GameID, &a.Code, &a.Name, &a.Status, &a.Config, &remark, &prod, &test, &review, &created, &updated)
    if remark.Valid { a.Remark = &remark.String }
    if prod.Valid { a.ProdServerGroupID = &prod.Int64 }
    if test.Valid { a.TestServerGroupID = &test.Int64 }
    if review.Valid { a.ReviewServerGroupID = &review.Int64 }
	a.CreatedAt, a.UpdatedAt = model.LocalDateTime(created), model.LocalDateTime(updated)
	return a, err
}

func validObject(config string) bool {
	var obj map[string]json.RawMessage
	return json.Unmarshal([]byte(config),&obj)==nil && obj!=nil
}

// apps 按所属游戏查询应用；管理员列表不查询已删除的绑定表。
func apps(ctx context.Context, db queryer, gameID int64) ([]model.ChannelApp,error) {
    statement := "SELECT "+appColumns+" FROM config_channel_app"
    var args []any
    if gameID>0 { statement+=" WHERE game_id=?"; args=append(args,gameID) }
    rows,err:=db.QueryContext(ctx,statement+" ORDER BY id",args...)
    if err!=nil { return nil,err }
    defer rows.Close()
    out:=make([]model.ChannelApp,0)
    for rows.Next() {
        app,err:=scanApp(rows)
        if err!=nil { return nil,err }
        out=append(out,app)
    }
    return out,rows.Err()
}

func (s *Store) ChannelApps(ctx context.Context, gameID int64) ([]model.ChannelApp,error) {
	if gameID < 0 { return nil,ErrInvalid }
	return apps(ctx,s.DB,gameID)
}

func validApp(name string,status *int,config string,remark *string) error {
	if service.ValidateNode(name,status,config,remark)!=nil || !validObject(config) { return ErrInvalid }
	return nil
}

// 渠道 CRUD 不影响节点同步；仍锁定全局版本行，以便与节点删除串行，防止孤儿关联。
func (s *Store) CreateChannelApp(ctx context.Context, input model.CreateChannelAppRequest) (model.ChannelApp,error) {
	if input.GameID<=0 || strings.TrimSpace(input.Code)=="" || len([]rune(input.Code))>128 ||
		validApp(input.Name,input.Status,input.Config,input.Remark)!=nil { return model.ChannelApp{},ErrInvalid }
	tx,err := s.DB.BeginTx(ctx,nil); if err!=nil { return model.ChannelApp{},err }; defer tx.Rollback()
	if err=lockRevision(ctx,tx); err!=nil { return model.ChannelApp{},err }
	var kind string
	if err=tx.QueryRowContext(ctx,"SELECT node_type FROM config_node WHERE id=?",input.GameID).Scan(&kind); err!=nil || kind!="game" { return model.ChannelApp{},ErrInvalid }
	if err := validateAppGroups(ctx,tx,input.GameID,input.ProdServerGroupID,input.TestServerGroupID,input.ReviewServerGroupID); err!=nil { return model.ChannelApp{},err }
    res,err := tx.ExecContext(ctx,"INSERT INTO config_channel_app(game_id,code,name,status,config,remark,prod_server_group_id,test_server_group_id,review_server_group_id,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))",input.GameID,input.Code,input.Name,*input.Status,input.Config,input.Remark,input.ProdServerGroupID,input.TestServerGroupID,input.ReviewServerGroupID)
	if err!=nil { return model.ChannelApp{},databaseError(err) }
	id,err:=res.LastInsertId(); if err!=nil { return model.ChannelApp{},err }
	a,err:=scanApp(tx.QueryRowContext(ctx,"SELECT "+appColumns+" FROM config_channel_app WHERE id=?",id))
	if err!=nil { return model.ChannelApp{},err }
	return a,tx.Commit()
}

func (s *Store) UpdateChannelApp(ctx context.Context,id int64,input model.UpdateChannelAppRequest) (model.ChannelApp,error) {
	if id<=0 || validApp(input.Name,input.Status,input.Config,input.Remark)!=nil { return model.ChannelApp{},ErrInvalid }
	tx,err:=s.DB.BeginTx(ctx,nil); if err!=nil { return model.ChannelApp{},err }; defer tx.Rollback()
	if err=lockRevision(ctx,tx); err!=nil { return model.ChannelApp{},err }
	existing,err:=scanApp(tx.QueryRowContext(ctx,"SELECT "+appColumns+" FROM config_channel_app WHERE id=?",id))
    if err!=nil { return model.ChannelApp{},err }
    if err:=validateAppGroups(ctx,tx,existing.GameID,input.ProdServerGroupID,input.TestServerGroupID,input.ReviewServerGroupID);err!=nil { return model.ChannelApp{},err }
    _,err=tx.ExecContext(ctx,"UPDATE config_channel_app SET name=?,status=?,config=?,remark=?,prod_server_group_id=?,test_server_group_id=?,review_server_group_id=?,updated_at=UTC_TIMESTAMP(6) WHERE id=?",input.Name,*input.Status,input.Config,input.Remark,input.ProdServerGroupID,input.TestServerGroupID,input.ReviewServerGroupID,id)
	if err!=nil { return model.ChannelApp{},databaseError(err) }
	a,err:=scanApp(tx.QueryRowContext(ctx,"SELECT "+appColumns+" FROM config_channel_app WHERE id=?",id))
	if err!=nil { return model.ChannelApp{},err }
	return a,tx.Commit()
}

func (s *Store) DeleteChannelApp(ctx context.Context,id int64) error {
	if id<=0 { return ErrInvalid }
	tx,err:=s.DB.BeginTx(ctx,nil); if err!=nil { return err }; defer tx.Rollback()
	if err=lockRevision(ctx,tx); err!=nil { return err }
	var count int
    if err=tx.QueryRowContext(ctx,"SELECT COUNT(*) FROM config_channel_app WHERE id=? AND (prod_server_group_id IS NOT NULL OR test_server_group_id IS NOT NULL OR review_server_group_id IS NOT NULL)",id).Scan(&count);err!=nil { return err }
    if count!=0 { return ErrConflict }
	if _,err=tx.ExecContext(ctx,"DELETE FROM config_channel_app WHERE id=?",id); err!=nil { return databaseError(err) }
	return tx.Commit()
}



// validateAppGroups 逐环境验证目标区服组的大区类型和游戏归属；在版本锁内与节点删除串行。
func validateAppGroups(ctx context.Context, tx *sql.Tx, gameID int64, prod, test, review *int64) error {
    targets := []struct { env string; id *int64 }{{"prod",prod},{"test",test},{"review",review}}
    for _,target := range targets {
        if target.id == nil { continue }
        if *target.id <= 0 { return ErrInvalid }
        var env string
        err := tx.QueryRowContext(ctx, "SELECT r.env_type FROM config_node sg JOIN config_node r ON r.id=sg.region_id WHERE sg.id=? AND sg.node_type='server_group' AND sg.game_id=? AND r.node_type='region' AND r.game_id=?",*target.id,gameID,gameID).Scan(&env)
        if err == sql.ErrNoRows { return ErrInvalid }
        if err != nil { return err }
        if env != target.env { return ErrInvalid }
    }
    return nil
}

// ServerGroupOptions 根据环境和关键词查询可选区服组，避免管理端加载整棵树。
func (s *Store) ServerGroupOptions(ctx context.Context, gameID int64, envType, keyword string) ([]model.ServerGroupOption,error) {
    if gameID<=0 || (envType!="prod" && envType!="test" && envType!="review") || len([]rune(keyword))>128 { return nil,ErrInvalid }
    pattern := "%"+escaped(strings.TrimSpace(keyword))+"%"
    rows,err:=s.DB.QueryContext(ctx, "SELECT sg.id,sg.name,sg.code,r.name FROM config_node sg JOIN config_node r ON r.id=sg.region_id WHERE sg.node_type='server_group' AND sg.game_id=? AND r.node_type='region' AND r.game_id=? AND r.env_type=? AND (sg.name LIKE ? ESCAPE '!' OR sg.code LIKE ? ESCAPE '!') ORDER BY r.id,sg.id LIMIT 100", gameID,gameID,envType,pattern,pattern)
    if err!=nil { return nil,err }
    defer rows.Close()
    out:=make([]model.ServerGroupOption,0)
    for rows.Next() {
        var option model.ServerGroupOption
        if err:=rows.Scan(&option.ID,&option.Name,&option.Code,&option.RegionName);err!=nil { return nil,err }
        out=append(out,option)
    }
    return out,rows.Err()
}

// ServerGroupBindings 从应用的三个环境外键计算区服组下的应用，只提供只读查看。
func (s *Store) ServerGroupBindings(ctx context.Context,groupID int64) ([]model.ServerGroupBinding,error) {
    if groupID<=0 { return nil,ErrInvalid }
    rows,err:=s.DB.QueryContext(ctx, "SELECT a.id,a.name,a.code,a.status,a.config FROM config_channel_app a WHERE a.prod_server_group_id=? OR a.test_server_group_id=? OR a.review_server_group_id=? ORDER BY a.name,a.id",groupID,groupID,groupID)
    if err!=nil { return nil,err }
    defer rows.Close()
    out:=make([]model.ServerGroupBinding,0)
    for rows.Next() {
        var item model.ServerGroupBinding
        item.ServerGroupID=groupID
        if err:=rows.Scan(&item.ChannelAppID,&item.ChannelAppName,&item.ChannelAppCode,&item.Status,&item.Config);err!=nil { return nil,err }
        out=append(out,item)
    }
    return out,rows.Err()
}
