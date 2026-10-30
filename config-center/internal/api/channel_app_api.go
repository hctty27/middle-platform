package api

import (
	"net/http"

	"github.com/hctty27/middle-platform/config-center/internal/model"
)

// channelApps 按游戏查询公共应用定义。
func (h *api) channelApps(w http.ResponseWriter,r *http.Request) {
	gameID,err:=param(r,"gameId",-1)
	if err!=nil || gameID<=0 { respond(w,nil,ErrInvalid); return }
	out,err:=h.reader.ChannelApps(r.Context(),gameID)
	respond(w,out,err)
}

// serverGroupOptions 返回指定游戏和环境中可选择的区服组。
func (h *api) serverGroupOptions(w http.ResponseWriter,r *http.Request) {
    gameID,err:=param(r,"gameId",-1)
    env:=r.URL.Query().Get("envType")
    if err!=nil || gameID<=0 { respond(w,nil,ErrInvalid);return }
    out,err:=h.reader.ServerGroupOptions(r.Context(),gameID,env,r.URL.Query().Get("keyword"))
    respond(w,out,err)
}

func (h *api) createChannelApp(w http.ResponseWriter,r *http.Request) {
	var input model.CreateChannelAppRequest
	if err:=decodeBody(w,r,&input); err!=nil { respond(w,nil,err); return }
	out,err:=h.writer.CreateChannelApp(r.Context(),input)
	if err!=nil { respond(w,nil,err); return }
	writeJSON(w,http.StatusCreated,out)
}

func (h *api) updateChannelApp(w http.ResponseWriter,r *http.Request) {
	appID,err:=id(r); if err!=nil || appID<=0 { respond(w,nil,ErrInvalid); return }
	var input model.UpdateChannelAppRequest
	if err=decodeBody(w,r,&input); err!=nil { respond(w,nil,err); return }
	out,err:=h.writer.UpdateChannelApp(r.Context(),appID,input)
	respond(w,out,err)
}

func (h *api) deleteChannelApp(w http.ResponseWriter,r *http.Request) {
	appID,err:=id(r); if err!=nil || appID<=0 { respond(w,nil,ErrInvalid); return }
	if err=h.writer.DeleteChannelApp(r.Context(),appID); err!=nil { respond(w,nil,err); return }
	w.WriteHeader(http.StatusNoContent)
}

// serverGroupBindings 查询当前区服组已绑定的渠道应用，供工作台直接管理。
func (h *api) serverGroupBindings(w http.ResponseWriter, r *http.Request) {
	groupID, err := id(r)
	if err != nil || groupID <= 0 { respond(w, nil, ErrInvalid); return }
	out, err := h.reader.ServerGroupBindings(r.Context(), groupID)
	respond(w, out, err)
}
