// Package store 提供配置中心权威 MySQL 的只读查询。
package store

import (
    "context"
    "database/sql"
    "encoding/json"
    "fmt"

    "github.com/hctty27/middle-platform/sdk-core/internal/model"
)

type Store struct { db *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{db:db} }

const appQuery = `
SELECT a.id, a.game_id, a.code, a.name, a.status, a.config, g.status, g.code
FROM config_channel_app a
JOIN config_node g ON g.id = a.game_id AND g.node_type = 'game'
WHERE a.code = ?
LIMIT 2`

const boundGroupQuery = `
SELECT r.code, sg.code, sg.config
FROM config_channel_app a
JOIN config_node sg ON sg.id = CASE ?
    WHEN 'prod' THEN a.prod_server_group_id
    WHEN 'test' THEN a.test_server_group_id
    WHEN 'review' THEN a.review_server_group_id
    ELSE NULL END
JOIN config_node r ON r.id = sg.region_id
WHERE a.id = ?
  AND sg.node_type = 'server_group'
  AND sg.status = 1
  AND sg.game_id = ?
  AND r.node_type = 'region'
  AND r.status = 1
  AND r.game_id = ?
  AND r.env_type = ?
LIMIT 1`

// Lookup 在同一个可重复读事务中获取当前环境的唯一绑定和相关配置。
func (s *Store) Lookup(ctx context.Context,appID,env string) (model.ChannelApp,*model.BoundGroup,error) {
    tx,err:=s.db.BeginTx(ctx,&sql.TxOptions{Isolation:sql.LevelRepeatableRead,ReadOnly:true})
    if err!=nil { return model.ChannelApp{},nil,fmt.Errorf("begin config query: %w",err) }
    defer tx.Rollback()

    rows,err:=tx.QueryContext(ctx,appQuery,appID)
    if err!=nil { return model.ChannelApp{},nil,fmt.Errorf("query channel app: %w",err) }
    var app model.ChannelApp
    var appStatus,gameStatus,count int
    for rows.Next() {
        var config []byte
        if err:=rows.Scan(&app.ID,&app.GameID,&app.Code,&app.Name,&appStatus,&config,&gameStatus,&app.GameCode);err!=nil {
            rows.Close()
            return model.ChannelApp{},nil,fmt.Errorf("scan channel app: %w",err)
        }
        app.Config=append(json.RawMessage(nil),config...)
        count++
    }
    scanErr:=rows.Err()
    rows.Close()
    if scanErr!=nil { return model.ChannelApp{},nil,fmt.Errorf("iterate channel apps: %w",scanErr) }
    if count>1 { return model.ChannelApp{},nil,model.ErrAmbiguous }
    if count==0 || appStatus!=1 || gameStatus!=1 { return model.ChannelApp{},nil,model.ErrNotFound }

    var regionCode,groupCode string
    var groupJSON []byte
    err=tx.QueryRowContext(ctx,boundGroupQuery,env,app.ID,app.GameID,app.GameID,env).
        Scan(&regionCode,&groupCode,&groupJSON)
    if err==sql.ErrNoRows { return model.ChannelApp{},nil,model.ErrNotFound }
    if err!=nil { return model.ChannelApp{},nil,fmt.Errorf("query bound server group: %w",err) }
    group:=&model.BoundGroup{
        RegionCode:regionCode,
        ServerGroupCode:groupCode,
        Config:append(json.RawMessage(nil),groupJSON...),
    }
    if err:=tx.Commit();err!=nil { return model.ChannelApp{},nil,fmt.Errorf("commit config read: %w",err) }
    return app,group,nil
}

// VerifySchema 在启动时验证包括映射数据来源在内的必需字段。
func (s *Store) VerifySchema(ctx context.Context) error {
    rows,err:=s.db.QueryContext(ctx,`
SELECT a.code, a.name, a.config, a.prod_server_group_id,
       a.test_server_group_id, a.review_server_group_id,
       g.code, r.code, sg.code, sg.config
FROM config_channel_app a
JOIN config_node g ON g.id=a.game_id
LEFT JOIN config_node sg ON sg.id=a.prod_server_group_id
LEFT JOIN config_node r ON r.id=sg.region_id
WHERE 1=0`)
    if err!=nil { return fmt.Errorf("verify sdk-core database schema: %w",err) }
    return rows.Close()
}
