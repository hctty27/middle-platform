package api

import (
    "net/http"
    "strconv"

    "github.com/go-chi/chi/v5"
    "github.com/hctty27/middle-platform/config-center/internal/model"
)

// sdkServices 统一列出内置的 SDK 服务定义。
func (h *api) sdkServices(w http.ResponseWriter, r *http.Request) {
    items,err := h.reader.SDKServices(r.Context())
    respond(w,items,err)
}

// sdkChannelServices 返回某渠道包在一个环境的所有服务开关。
func (h *api) sdkChannelServices(w http.ResponseWriter, r *http.Request) {
    appID,err:=id(r)
    if err!=nil || appID<=0 { respond(w,nil,ErrInvalid);return }
    env:=r.URL.Query().Get("env")
    if env!="prod"&&env!="test"&&env!="review" { respond(w,nil,ErrInvalid);return }
    items,err:=h.reader.SDKChannelServices(r.Context(),appID,env)
    respond(w,items,err)
}

// updateSDKService 仅更新全局启用状态。
func (h *api) updateSDKService(w http.ResponseWriter, r *http.Request) {
    serviceID,err:=id(r)
    if err!=nil || serviceID<=0 { respond(w,nil,ErrInvalid);return }
    var req model.UpdateSDKServiceRequest
    if err=decodeBody(w,r,&req);err!=nil || req.Enabled==nil { respond(w,nil,ErrInvalid);return }
    if err=h.writer.UpdateSDKService(r.Context(),serviceID,*req.Enabled);err!=nil { respond(w,nil,err);return }
    w.WriteHeader(http.StatusNoContent)
}

// upsertSDKChannelService 保存渠道包在指定环境下的服务设置。
func (h *api) upsertSDKChannelService(w http.ResponseWriter, r *http.Request) {
    appID,err:=id(r)
    if err!=nil || appID<=0 { respond(w,nil,ErrInvalid);return }
    serviceID,err:=strconv.ParseInt(chi.URLParam(r,"serviceId"),10,64)
    if err!=nil || serviceID<=0 { respond(w,nil,ErrInvalid);return }
    env:=r.URL.Query().Get("env")
    if env!="prod"&&env!="test"&&env!="review" { respond(w,nil,ErrInvalid);return }
    var req model.UpsertSDKChannelServiceRequest
    if err=decodeBody(w,r,&req);err!=nil || req.Enabled==nil { respond(w,nil,ErrInvalid);return }
    if err=h.writer.UpsertSDKChannelService(r.Context(),appID,serviceID,env,*req.Enabled,req.Config);err!=nil {
        respond(w,nil,err)
        return
    }
    w.WriteHeader(http.StatusNoContent)
}
