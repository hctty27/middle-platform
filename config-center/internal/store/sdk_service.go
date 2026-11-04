package store

import (
    "bytes"
    "context"
    "database/sql"
    "encoding/json"
    "errors"
    "io"
    "time"

    "github.com/hctty27/middle-platform/config-center/internal/model"
)

const (
    ScopeChannelApp = "channel_app"
    ScopeServerGroup = "server_group"
)

func validSDKScope(scope string) bool {
    return scope == ScopeChannelApp || scope == ScopeServerGroup
}

func validSDKEnv(env string) bool {
    return env == "prod" || env == "test" || env == "review"
}

const sdkServiceColumns = "id,code,name,scope_type,description,enabled,created_at,updated_at"

// SDKServices 返回全局服务目录，使用 ID 排序，不再使用 sort_order。
func (s *Store) SDKServices(ctx context.Context) ([]model.SDKService, error) {
    rows, err := s.DB.QueryContext(ctx, "SELECT "+sdkServiceColumns+" FROM sdk_service ORDER BY id")
    if err != nil { return nil, err }
    defer rows.Close()
    items := make([]model.SDKService, 0)
    for rows.Next() {
        var item model.SDKService
        var enabled int
        var created, updated time.Time
        if err := rows.Scan(&item.ID,&item.Code,&item.Name,&item.ScopeType,
            &item.Description,&enabled,&created,&updated); err != nil {return nil,err}
        item.Enabled = enabled == 1
        item.CreatedAt, item.UpdatedAt = model.LocalDateTime(created),model.LocalDateTime(updated)
        items = append(items,item)
    }
    return items,rows.Err()
}

func (s *Store) UpdateSDKService(ctx context.Context, id int64, enabled bool) error {
    if id <= 0 { return ErrInvalid }
    result, err := s.DB.ExecContext(ctx,
        "UPDATE sdk_service SET enabled=?,updated_at=UTC_TIMESTAMP(3) WHERE id=?",enabled,id)
    if err != nil { return databaseError(err) }
    affected,err:=result.RowsAffected()
    if err != nil {return err}
    if affected>0 {return nil}
    var exists int
    return s.DB.QueryRowContext(ctx,"SELECT 1 FROM sdk_service WHERE id=?",id).Scan(&exists)
}

// targetState 检查目标存在及环境归属；目标停用时仍允许预先配置，但生效状态为关闭。
func targetState(ctx context.Context, db queryer, scope string, targetID int64, env string) (bool,error) {
    var statuses [4]sql.NullInt64
    switch scope {
    case ScopeChannelApp:
        err:=db.QueryRowContext(ctx,`
SELECT app.status,g.status,sg.status,r.status FROM config_channel_app app
LEFT JOIN config_node g ON g.id=app.game_id AND g.node_type='game'
LEFT JOIN config_node sg ON sg.id=CASE ?
  WHEN 'prod' THEN app.prod_server_group_id
  WHEN 'test' THEN app.test_server_group_id
  WHEN 'review' THEN app.review_server_group_id
END AND sg.node_type='server_group' AND sg.game_id=app.game_id
LEFT JOIN config_node r ON r.id=sg.region_id AND r.node_type='region'
 AND r.game_id=app.game_id AND r.env_type=?
WHERE app.id=?`,env,env,targetID).Scan(&statuses[0],&statuses[1],&statuses[2],&statuses[3])
        if err!=nil{return false,err}
    case ScopeServerGroup:
        err:=db.QueryRowContext(ctx,`
SELECT sg.status,r.status,g.status
FROM config_node sg
JOIN config_node r ON r.id=sg.region_id AND r.node_type='region' AND r.env_type=?
JOIN config_node g ON g.id=sg.game_id AND g.node_type='game' AND r.game_id=g.id
WHERE sg.id=? AND sg.node_type='server_group' AND sg.game_id=r.game_id`,env,targetID).
            Scan(&statuses[0],&statuses[1],&statuses[2])
        if errors.Is(err,sql.ErrNoRows){return false,ErrInvalid}
        if err!=nil{return false,err}
        statuses[3]=sql.NullInt64{Int64:1,Valid:true}
    default:
        return false,ErrInvalid
    }
    for _,v:=range statuses {
        if !v.Valid || v.Int64!=1 {return false,nil}
    }
    return true,nil
}

// SDKServiceBindings 按 scope_type 过滤服务目录并读取指定目标在某环境的绑定。
func (s *Store) SDKServiceBindings(ctx context.Context,scope string,targetID int64,env string)([]model.SDKBindingService,error) {
    if !validSDKScope(scope)||targetID<=0||!validSDKEnv(env){return nil,ErrInvalid}
    active,err:=targetState(ctx,s.DB,scope,targetID,env)
    if err!=nil{return nil,err}
    rows,err:=s.DB.QueryContext(ctx,`
SELECT sv.id,sv.code,sv.name,sv.description,sv.enabled,
       COALESCE(b.enabled,0),COALESCE(b.config,JSON_OBJECT())
FROM sdk_service sv
LEFT JOIN sdk_service_binding b ON b.service_id=sv.id AND b.target_id=? AND b.env=?
WHERE sv.scope_type=?
ORDER BY sv.id`,targetID,env,scope)
    if err!=nil{return nil,err}
    defer rows.Close()
    items:=make([]model.SDKBindingService,0)
    for rows.Next(){
        var item model.SDKBindingService
        var globalEnabled,bindingEnabled int
        var raw []byte
        if err:=rows.Scan(&item.ServiceID,&item.Code,&item.Name,&item.Description,
            &globalEnabled,&bindingEnabled,&raw);err!=nil{return nil,err}
        item.GlobalEnabled=globalEnabled==1
        item.Enabled=bindingEnabled==1
        item.EffectiveEnabled=item.GlobalEnabled && item.Enabled && active
        item.Config=append(json.RawMessage(nil),raw...)
        items=append(items,item)
    }
    return items,rows.Err()
}

// SDKChannelServices 兼容现有管理端路径，底层共享通用查询。
func (s *Store) SDKChannelServices(ctx context.Context,appID int64,env string)([]model.SDKChannelService,error){
    return s.SDKServiceBindings(ctx,ScopeChannelApp,appID,env)
}

func validBindingConfig(config json.RawMessage) bool {
    if len(config)==0 || len(config)>64<<10{return false}
    decoder:=json.NewDecoder(bytes.NewReader(config))
    var object map[string]json.RawMessage
    if err:=decoder.Decode(&object);err!=nil||object==nil{return false}
    return decoder.Decode(new(any))==io.EOF
}

// UpsertSDKServiceBinding 以配置版本行串行化绑定写入与目标删除，校验目标类型及环境。
func (s *Store) UpsertSDKServiceBinding(ctx context.Context,scope string,targetID,serviceID int64,
    env string,enabled bool,config json.RawMessage)error {
    if !validSDKScope(scope)||targetID<=0||serviceID<=0||!validSDKEnv(env)||
        !validBindingConfig(config){return ErrInvalid}
    tx,err:=s.DB.BeginTx(ctx,nil)
    if err!=nil{return err}
    defer tx.Rollback()
    if err=lockRevision(ctx,tx);err!=nil{return err}

    var actualScope string
    if err=tx.QueryRowContext(ctx,"SELECT scope_type FROM sdk_service WHERE id=?",serviceID).
        Scan(&actualScope);err!=nil{return err}
    if actualScope!=scope{return ErrInvalid}
    if _,err=targetState(ctx,tx,scope,targetID,env);err!=nil{return err}
    _,err=tx.ExecContext(ctx,`
INSERT INTO sdk_service_binding (service_id,target_id,env,enabled,config)
VALUES (?,?,?,?,?)
ON DUPLICATE KEY UPDATE enabled=VALUES(enabled),config=VALUES(config),updated_at=UTC_TIMESTAMP(3)`,serviceID,targetID,env,enabled,string(config))
    if err!=nil{return databaseError(err)}
    return tx.Commit()
}

// UpsertSDKChannelService 兼容当前管理端 API。
func (s *Store) UpsertSDKChannelService(ctx context.Context,appID,serviceID int64,
    env string,enabled bool,config json.RawMessage)error {
    return s.UpsertSDKServiceBinding(ctx,ScopeChannelApp,appID,serviceID,env,enabled,config)
}
