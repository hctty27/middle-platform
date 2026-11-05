package api

import (
    "database/sql"
    "encoding/json"
    "errors"
    "net/http"
    "strconv"
    "time"
)

type serviceEntry struct {
    ID int64 `json:"id"`
    Code string `json:"code"`
    Name string `json:"name"`
    Description string `json:"description"`
    Enabled bool `json:"enabled"`
    CreatedAt time.Time `json:"createdAt"`
    UpdatedAt time.Time `json:"updatedAt"`
}

func validAdminEnv(env string)bool {return env=="prod"||env=="test"||env=="review"}

func registerServiceAdminRoutes(mux *http.ServeMux,h *adminStore){
    mux.HandleFunc("GET /api/core/admin/services", h.services)
    mux.HandleFunc("PUT /api/core/admin/services/{id}", h.updateService)
    mux.HandleFunc("GET /api/core/admin/channel-apps/{id}/services", h.channelServices)
    mux.HandleFunc("PUT /api/core/admin/channel-apps/{id}/services/{serviceId}", h.updateChannelService)
}
func(h *adminStore) services(w http.ResponseWriter,r *http.Request){
    rows,err:=h.db.QueryContext(r.Context(),`SELECT id,code,name,description,enabled,created_at,updated_at FROM sdk_service ORDER BY id`)
    if err!=nil{adminError(w,500,"加载 SDK 服务目录失败");return}
    defer rows.Close()
    items:=make([]serviceEntry,0)
    for rows.Next(){
        var e serviceEntry
        var enabled int
        if rows.Scan(&e.ID,&e.Code,&e.Name,&e.Description,&enabled,&e.CreatedAt,&e.UpdatedAt)!=nil{
            adminError(w,500,"读取 SDK 服务目录失败");return
        }
        e.Enabled=enabled==1
        items=append(items,e)
    }
    if rows.Err()!=nil{adminError(w,500,"读取 SDK 服务目录失败");return}
    writeJSON(w,200,items)
}
func(h *adminStore) updateService(w http.ResponseWriter,r *http.Request){
    id,ok:=adminID(r)
    if !ok{adminError(w,400,"服务 ID 不合法");return}
    var input struct{Enabled *bool `json:"enabled"`}
    if !adminDecode(w,r,&input){return}
    if input.Enabled==nil{adminError(w,400,"必须提供 enabled");return}
    result,err:=h.db.ExecContext(r.Context(),`UPDATE sdk_service SET enabled=?,updated_at=UTC_TIMESTAMP(3) WHERE id=?`,*input.Enabled,id)
    if err!=nil{adminError(w,500,"更新 SDK 服务失败");return}
    affected,_:=result.RowsAffected()
    if affected==0 {
        var exists int
        if h.db.QueryRowContext(r.Context(),"SELECT 1 FROM sdk_service WHERE id=?",id).Scan(&exists)!=nil{
            adminError(w,404,"服务不存在");return
        }
    }
    w.WriteHeader(204)
}
func(h *adminStore) channelServices(w http.ResponseWriter,r *http.Request){
    id,ok:=adminID(r)
    env:=r.URL.Query().Get("env")
    if !ok||!validAdminEnv(env){adminError(w,400,"渠道包或环境不合法");return}
    var active,exists int
    err:=h.db.QueryRowContext(r.Context(),"SELECT status,1 FROM sdk_channel_app WHERE id=?",id).Scan(&active,&exists)
    if errors.Is(err,sql.ErrNoRows){adminError(w,404,"渠道包不存在");return}
    if err!=nil{adminError(w,500,"查询渠道包失败");return}
    rows,err:=h.db.QueryContext(r.Context(),`SELECT sv.id,sv.code,sv.name,sv.description,sv.enabled,
       COALESCE(ch.enabled,0),COALESCE(ch.config,JSON_OBJECT()) FROM sdk_service sv
       LEFT JOIN sdk_service_channel ch ON ch.service_id=sv.id AND ch.channel_app_id=? AND ch.env=?
       ORDER BY sv.id`,id,env)
    if err!=nil{adminError(w,500,"查询渠道服务开关失败");return}
    defer rows.Close()
    items:=make([]map[string]any,0)
    for rows.Next(){
        var serviceID int64
        var code,name,description string
        var global,enabled int
        var raw []byte
        if rows.Scan(&serviceID,&code,&name,&description,&global,&enabled,&raw)!=nil{
           adminError(w,500,"读取渠道服务配置失败");return
        }
        items=append(items,map[string]any{
            "serviceId":serviceID,"code":code,"name":name,"description":description,
            "globalEnabled":global==1,"enabled":enabled==1,
            "effectiveEnabled":global==1 && enabled==1 && active==1,
            "config":copyRaw(raw),
        })
    }
    if rows.Err()!=nil{adminError(w,500,"读取渠道服务配置失败");return}
    writeJSON(w,200,items)
}
func(h *adminStore) updateChannelService(w http.ResponseWriter,r *http.Request){
    id,ok:=adminID(r)
    env:=r.URL.Query().Get("env")
    serviceID,err:=strconv.ParseInt(r.PathValue("serviceId"),10,64)
    if !ok||!validAdminEnv(env)||err!=nil||serviceID<=0{adminError(w,400,"渠道包、服务或环境参数不合法");return}
    var input struct {
       Enabled *bool `json:"enabled"`
       Config json.RawMessage `json:"config"`
    }
    if !adminDecode(w,r,&input){return}
    if input.Enabled==nil{adminError(w,400,"必须提供 enabled");return}
    if _,ok:=jsonObject(string(input.Config));!ok{adminError(w,400,"扩展配置必须是 JSON 对象");return}
    ctx:=r.Context()
    tx,err:=h.db.BeginTx(ctx,nil)
    if err!=nil{adminError(w,500,"开启事务失败");return}
    defer tx.Rollback()
    if txLockRevision(ctx,tx)!=nil{adminError(w,500,"锁定配置版本失败");return}
    var n int
    if err=tx.QueryRowContext(ctx,"SELECT 1 FROM sdk_channel_app WHERE id=?",id).Scan(&n);err!=nil{
       adminError(w,404,"渠道包不存在");return
    }
    if err=tx.QueryRowContext(ctx,"SELECT 1 FROM sdk_service WHERE id=?",serviceID).Scan(&n);err!=nil{
       adminError(w,404,"SDK 服务不存在");return
    }
    _,err=tx.ExecContext(ctx,`INSERT INTO sdk_service_channel
       (service_id,channel_app_id,env,enabled,config) VALUES(?,?,?,?,?)
       ON DUPLICATE KEY UPDATE enabled=VALUES(enabled),config=VALUES(config),updated_at=UTC_TIMESTAMP(3)`,
       serviceID,id,env,*input.Enabled,[]byte(input.Config))
    if err!=nil{adminError(w,500,"保存渠道服务配置失败");return}
    if tx.Commit()!=nil{adminError(w,500,"提交渠道服务配置失败");return}
    w.WriteHeader(204)
}
