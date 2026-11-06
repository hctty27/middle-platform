package api

import (
    "context"
    "database/sql"
    "errors"
    "net/http"
    "strconv"
    "strings"
    "time"
)

// channelApp 是管理接口返回的渠道包及环境绑定视图。
type channelApp struct {
    ID int64 `json:"id"`
    GameID int64 `json:"gameId"`
    Code string `json:"code"`
    Name string `json:"name"`
    Status int `json:"status"`
    Config string `json:"config"`
    Remark *string `json:"remark"`
    ProdServerGroupID *int64 `json:"prodServerGroupId"`
    TestServerGroupID *int64 `json:"testServerGroupId"`
    ReviewServerGroupID *int64 `json:"reviewServerGroupId"`
    CreatedAt time.Time `json:"createdAt"`
    UpdatedAt time.Time `json:"updatedAt"`
}
// channelWrite 只接收渠道包可编辑字段，环境绑定通过区服组 ID 指定。
type channelWrite struct {
    GameID int64 `json:"gameId"`
    Code string `json:"code"`
    Name string `json:"name"`
    Status *int `json:"status"`
    Config string `json:"config"`
    Remark *string `json:"remark"`
    ProdServerGroupID *int64 `json:"prodServerGroupId"`
    TestServerGroupID *int64 `json:"testServerGroupId"`
    ReviewServerGroupID *int64 `json:"reviewServerGroupId"`
}
// channelColumns 必须与 scanChannel 的扫描顺序一致。
const channelColumns = "id,game_id,code,name,status,config,remark,prod_server_group_id,test_server_group_id,review_server_group_id,created_at,updated_at"

// registerChannelAdminRoutes 注册渠道包管理及环境区服组绑定查询接口。
func registerChannelAdminRoutes(mux *http.ServeMux, h *adminStore) {
    mux.HandleFunc("GET /api/core/admin/channel-apps", h.channelApps)
    mux.HandleFunc("POST /api/core/admin/channel-apps", h.createChannelApp)
    mux.HandleFunc("PUT /api/core/admin/channel-apps/{id}", h.updateChannelApp)
    mux.HandleFunc("DELETE /api/core/admin/channel-apps/{id}", h.deleteChannelApp)
    mux.HandleFunc("GET /api/core/admin/channel-apps/server-groups", h.serverGroupOptions)
    mux.HandleFunc("GET /api/core/admin/server-groups/{id}/channel-apps", h.serverGroupBindings)
}

// scanChannel 将数据库可空字段转换为管理接口的可选 JSON 字段。
func scanChannel(row interface{ Scan(...any)error }) (channelApp,error) {
    var a channelApp
    var remark sql.NullString
    var prod,test,review sql.NullInt64
    err:=row.Scan(&a.ID,&a.GameID,&a.Code,&a.Name,&a.Status,&a.Config,&remark,
      &prod,&test,&review,&a.CreatedAt,&a.UpdatedAt)
    if remark.Valid {a.Remark=&remark.String}
    if prod.Valid {a.ProdServerGroupID=&prod.Int64}
    if test.Valid {a.TestServerGroupID=&test.Int64}
    if review.Valid {a.ReviewServerGroupID=&review.Int64}
    return a,err
}

// validChannelWrite 校验渠道包名称、状态、备注长度及 JSON 配置格式。
func validChannelWrite(req channelWrite) bool {
    if strings.TrimSpace(req.Name)=="" || len([]rune(req.Name))>128 ||
       req.Status==nil || (*req.Status!=0 && *req.Status!=1) ||
       len([]rune(req.RemarkValue()))>500 {return false}
    _,valid:=jsonObject(req.Config)
    return valid
}
// RemarkValue 返回可选备注的实际值，用于统一校验长度。
func (r channelWrite) RemarkValue() string {
    if r.Remark==nil {return ""};return *r.Remark
}
// validateChannelGroups 逐一验证三个环境绑定的区服组属于同一游戏且环境一致。
func validateChannelGroups(ctx context.Context, tx *sql.Tx,gameID int64,req channelWrite) error {
    for _,b:=range []struct{env string;id *int64}{
        {"prod",req.ProdServerGroupID},{"test",req.TestServerGroupID},{"review",req.ReviewServerGroupID},
    } {
        if b.id==nil {continue}
        if *b.id<=0 {return errors.New("区服组 ID 不合法")}
        var env sql.NullString
        err:=tx.QueryRowContext(ctx,`SELECT r.env_type FROM config_node sg
           JOIN config_node r ON r.id=sg.region_id AND r.node_type='region' AND r.game_id=?
           WHERE sg.id=? AND sg.node_type='server_group' AND sg.game_id=?`,gameID,*b.id,gameID).Scan(&env)
        if err!=nil || !env.Valid || env.String!=b.env {return errors.New("区服组与游戏或环境不匹配")}
    }
    return nil
}

func (h *adminStore) channelApps(w http.ResponseWriter,r *http.Request) {
    gameID,err:=parseAdminQueryID(r,"gameId")
    if err!=nil || gameID<=0 {adminError(w,400,"请选择游戏");return}
    rows,err:=h.db.QueryContext(r.Context(),"SELECT "+channelColumns+" FROM sdk_channel_app WHERE game_id=? ORDER BY id",gameID)
    if err!=nil {adminError(w,500,"查询渠道包失败");return}
    defer rows.Close()
    items:=make([]channelApp,0)
    for rows.Next() {
        a,e:=scanChannel(rows)
        if e!=nil {adminError(w,500,"读取渠道包失败");return}
        items=append(items,a)
    }
    if rows.Err()!=nil {adminError(w,500,"读取渠道包失败");return}
    writeJSON(w,200,items)
}
func parseAdminQueryID(r *http.Request,key string)(int64,error){
    return strconv.ParseInt(r.URL.Query().Get(key),10,64)
}

// createChannelApp 在事务中锁定配置版本并校验绑定，避免并发写入导致状态不一致。
func (h *adminStore) createChannelApp(w http.ResponseWriter,r *http.Request){
    var req channelWrite
    if !adminDecode(w,r,&req){return}
    if req.GameID<=0 || strings.TrimSpace(req.Code)=="" || len([]rune(req.Code))>128 || !validChannelWrite(req) {
        adminError(w,400,"渠道包信息不合法");return
    }
    ctx:=r.Context()
    tx,err:=h.db.BeginTx(ctx,nil)
    if err!=nil {adminError(w,500,"开启事务失败");return}
    defer tx.Rollback()
    if txLockRevision(ctx,tx)!=nil {adminError(w,500,"锁定配置版本失败");return}
    var kind string
    if err=tx.QueryRowContext(ctx,"SELECT node_type FROM config_node WHERE id=?",req.GameID).Scan(&kind);err!=nil||kind!="game" {
        adminError(w,400,"游戏节点不存在");return
    }
    if validateChannelGroups(ctx,tx,req.GameID,req)!=nil {adminError(w,400,"绑定区服组不合法");return}
    result,err:=tx.ExecContext(ctx,`INSERT INTO sdk_channel_app
      (game_id,code,name,status,config,remark,prod_server_group_id,test_server_group_id,review_server_group_id,created_at,updated_at)
      VALUES(?,?,?,?,?,?,?,?,?,UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))`,
      req.GameID,req.Code,req.Name,*req.Status,req.Config,nullableText(req.Remark),
      nullableID(req.ProdServerGroupID),nullableID(req.TestServerGroupID),nullableID(req.ReviewServerGroupID))
    if err!=nil {adminError(w,409,"渠道包编码重复或违反数据库约束");return}
    id,err:=result.LastInsertId()
    if err!=nil {adminError(w,500,"读取新渠道包 ID 失败");return}
    item,err:=scanChannel(tx.QueryRowContext(ctx,"SELECT "+channelColumns+" FROM sdk_channel_app WHERE id=?",id))
    if err!=nil {adminError(w,500,"读取新渠道包失败");return}
    if tx.Commit()!=nil {adminError(w,500,"提交渠道包失败");return}
    writeJSON(w,201,item)
}

// updateChannelApp 保持渠道包所属游戏不变，只更新允许修改的字段及环境绑定。
func (h *adminStore) updateChannelApp(w http.ResponseWriter,r *http.Request) {
    id,ok:=adminID(r)
    if !ok {adminError(w,400,"渠道包 ID 不合法");return}
    var req channelWrite
    if !adminDecode(w,r,&req){return}
    if !validChannelWrite(req) {adminError(w,400,"渠道包信息不合法");return}
    ctx:=r.Context()
    tx,err:=h.db.BeginTx(ctx,nil)
    if err!=nil {adminError(w,500,"开启事务失败");return}
    defer tx.Rollback()
    if txLockRevision(ctx,tx)!=nil {adminError(w,500,"锁定配置版本失败");return}
    var gameID int64
    err=tx.QueryRowContext(ctx,"SELECT game_id FROM sdk_channel_app WHERE id=?",id).Scan(&gameID)
    if errors.Is(err,sql.ErrNoRows){adminError(w,404,"渠道包不存在");return}
    if err!=nil {adminError(w,500,"查询渠道包失败");return}
    if validateChannelGroups(ctx,tx,gameID,req)!=nil {adminError(w,400,"绑定区服组不合法");return}
    _,err=tx.ExecContext(ctx,`UPDATE sdk_channel_app SET name=?,status=?,config=?,remark=?,
      prod_server_group_id=?,test_server_group_id=?,review_server_group_id=?,
      updated_at=UTC_TIMESTAMP(6) WHERE id=?`,req.Name,*req.Status,req.Config,nullableText(req.Remark),
      nullableID(req.ProdServerGroupID),nullableID(req.TestServerGroupID),nullableID(req.ReviewServerGroupID),id)
    if err!=nil {adminError(w,409,"更新渠道包违反数据库约束");return}
    item,err:=scanChannel(tx.QueryRowContext(ctx,"SELECT "+channelColumns+" FROM sdk_channel_app WHERE id=?",id))
    if err!=nil {adminError(w,500,"查询渠道包失败");return}
    if tx.Commit()!=nil {adminError(w,500,"提交渠道包失败");return}
    writeJSON(w,200,item)
}

// deleteChannelApp 要求先解除环境绑定，再删除渠道功能关联及渠道包记录。
func (h *adminStore) deleteChannelApp(w http.ResponseWriter,r *http.Request) {
    id,ok:=adminID(r)
    if !ok {adminError(w,400,"渠道包 ID 不合法");return}
    ctx:=r.Context()
    tx,err:=h.db.BeginTx(ctx,nil)
    if err!=nil {adminError(w,500,"开启事务失败");return}
    defer tx.Rollback()
    if txLockRevision(ctx,tx)!=nil {adminError(w,500,"锁定配置版本失败");return}
    var prod,test,review sql.NullInt64
    err=tx.QueryRowContext(ctx,`SELECT prod_server_group_id,test_server_group_id,review_server_group_id
        FROM sdk_channel_app WHERE id=? FOR UPDATE`,id).Scan(&prod,&test,&review)
    if errors.Is(err,sql.ErrNoRows){adminError(w,404,"渠道包不存在");return}
    if err!=nil {adminError(w,500,"查询渠道包失败");return}
    if prod.Valid||test.Valid||review.Valid {adminError(w,409,"请先解除三个环境的绑定");return}
    if _,err=tx.ExecContext(ctx,"DELETE FROM sdk_service_channel WHERE channel_app_id=?",id);err!=nil {
       adminError(w,500,"清理功能绑定失败");return
    }
    if _,err=tx.ExecContext(ctx,"DELETE FROM sdk_channel_app WHERE id=?",id);err!=nil {
       adminError(w,500,"删除渠道包失败");return
    }
    if tx.Commit()!=nil {adminError(w,500,"提交删除失败");return}
    w.WriteHeader(204)
}

// serverGroupOptions 仅返回指定游戏与环境的区服组，并对 LIKE 搜索字符转义。
func (h *adminStore) serverGroupOptions(w http.ResponseWriter,r *http.Request){
    gameID,err:=parseAdminQueryID(r,"gameId")
    env:=r.URL.Query().Get("envType")
    if err!=nil||gameID<=0||!validAdminEnv(env) {adminError(w,400,"游戏或环境参数不合法");return}
    keyword:=strings.TrimSpace(r.URL.Query().Get("keyword"))
    if len([]rune(keyword))>128 {adminError(w,400,"搜索内容过长");return}
    keyword=strings.NewReplacer("!","!!","%","!%","_","!_").Replace(keyword)
    rows,err:=h.db.QueryContext(r.Context(),`SELECT sg.id,sg.name,sg.code,r.name FROM config_node sg
      JOIN config_node r ON r.id=sg.region_id
      WHERE sg.node_type='server_group' AND sg.game_id=? AND r.node_type='region'
      AND r.game_id=? AND r.env_type=?
      AND (sg.name LIKE ? ESCAPE '!' OR sg.code LIKE ? ESCAPE '!')
      ORDER BY r.id,sg.id LIMIT 100`,gameID,gameID,env,"%"+keyword+"%","%"+keyword+"%")
    if err!=nil {adminError(w,500,"查询区服组失败");return}
    defer rows.Close()
    out:=make([]map[string]any,0)
    for rows.Next(){
       var id int64
       var name,code,regionName string
       if rows.Scan(&id,&name,&code,&regionName)!=nil {adminError(w,500,"读取区服组失败");return}
       out=append(out,map[string]any{"id":id,"name":name,"code":code,"regionName":regionName})
    }
    if rows.Err()!=nil {adminError(w,500,"读取区服组失败");return}
    writeJSON(w,200,out)
}

// serverGroupBindings 查询绑定了指定区服组的渠道包，不区分绑定环境。
func (h *adminStore) serverGroupBindings(w http.ResponseWriter,r *http.Request){
    id,ok:=adminID(r)
    if !ok {adminError(w,400,"区服组 ID 不合法");return}
    rows,err:=h.db.QueryContext(r.Context(),`SELECT id,name,code,status,config FROM sdk_channel_app
        WHERE prod_server_group_id=? OR test_server_group_id=? OR review_server_group_id=?
        ORDER BY name,id`,id,id,id)
    if err!=nil {adminError(w,500,"查询关联渠道包失败");return}
    defer rows.Close()
    out:=make([]map[string]any,0)
    for rows.Next(){
       var appID int64
       var name,code,config string
       var status int
       if rows.Scan(&appID,&name,&code,&status,&config)!=nil {adminError(w,500,"读取渠道包失败");return}
       out=append(out,map[string]any{"channelAppId":appID,"channelAppName":name,
         "channelAppCode":code,"serverGroupId":id,"status":status,"config":config})
    }
    if rows.Err()!=nil {adminError(w,500,"读取渠道包失败");return}
    writeJSON(w,200,out)
}
