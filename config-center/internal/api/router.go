// Package api 处理配置中心的 HTTP 路由及响应。
package api

import (
	"context"
	"net/http"
	"time"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/hctty27/middle-platform/config-center/internal/model"
)

// HTTP 接口使用统一的领域类型，保持既有 JSON 字段协议。
type Node = model.Node
type NodeSummary = model.NodeSummary
type NodePage = model.NodePage
type NodeMatch = model.NodeMatch
type Revision = model.Revision
type ChangeEvent = model.ChangeEvent
type Snapshot = model.Snapshot
type Subscriber = model.Subscriber
type CreateNodeRequest = model.CreateNodeRequest
type UpdateNodeRequest = model.UpdateNodeRequest
type CreateSubscriberRequest = model.CreateSubscriberRequest
var ErrInvalid = model.ErrInvalid
var ErrConflict = model.ErrConflict

// Reader 定义只读接口所依赖的存储能力。
type Reader interface {
	Ready(context.Context) error
	Nodes(context.Context) ([]Node, error)
	Node(context.Context, int64) (Node, error)
	Batch(context.Context, []int64) ([]Node, error)
	Tree(context.Context, int64) ([]NodeSummary, error)
	Children(context.Context, int64, int, int, string, string) (NodePage, error)
	Search(context.Context, string, int) ([]NodeMatch, error)
	Path(context.Context, int64) (NodeMatch, error)
	Revision(context.Context) (Revision, error)
	Changes(context.Context, int64, int) ([]ChangeEvent, error)
	Snapshot(context.Context) (Snapshot, error)
	Subscribers(context.Context) ([]Subscriber, error)
	ChannelApps(context.Context,int64) ([]model.ChannelApp,error)
	ServerGroupOptions(context.Context,int64,string,string) ([]model.ServerGroupOption,error)
	ServerGroupBindings(context.Context,int64) ([]model.ServerGroupBinding,error)
}

// Writer 定义可选的管理写操作。
type Writer interface {
	Create(context.Context, CreateNodeRequest) (Node, error)
	Update(context.Context, int64, UpdateNodeRequest) (Node, error)
	Delete(context.Context, int64) error
	CreateSubscriber(context.Context, CreateSubscriberRequest) error
	CreateChannelApp(context.Context,model.CreateChannelAppRequest) (model.ChannelApp,error)
	UpdateChannelApp(context.Context,int64,model.UpdateChannelAppRequest) (model.ChannelApp,error)
	DeleteChannelApp(context.Context,int64) error
}

// api 保存 HTTP 路由处理所需的读写依赖。
type api struct {
	reader Reader
	writer Writer
}

// Router 注册默认的只读路由。
func Router(reader Reader) http.Handler { return router(reader, nil) }

// RouterWithWrites 额外启用管理写接口。
func RouterWithWrites(reader Reader, writer Writer) http.Handler { return router(reader, writer) }

// router 集中注册所有配置中心 HTTP 接口。
func router(reader Reader, writer Writer) http.Handler {
	h := &api{reader: reader, writer: writer}
	r := chi.NewRouter()
	r.Use(middleware.RequestID, middleware.Recoverer, middleware.Logger)
	r.Get("/health/live", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, http.StatusOK, map[string]string{"status": "UP"}) })
	r.Get("/health/ready", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := h.reader.Ready(ctx); err != nil {
			writeError(w, http.StatusServiceUnavailable, "database unavailable")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "UP"})
	})
	// 管理查询和接入同步共享同一服务前缀，由 Nginx 在更外层区分业务模块。
	r.Get("/api/config/admin/nodes", h.list)
	r.Get("/api/config/admin/nodes/tree", h.tree)
	r.Get("/api/config/admin/nodes/children", h.children)
	r.Get("/api/config/admin/nodes/search", h.search)
	r.Get("/api/config/admin/nodes/{id}/children", h.childrenByID)
	r.Get("/api/config/admin/nodes/{id}/path", h.path)
	r.Get("/api/config/admin/nodes/{id}", h.node)
	r.Get("/api/config/admin/subscribers", h.subscribers)
	r.Get("/api/config/admin/channel-apps",h.channelApps)
	r.Get("/api/config/admin/channel-apps/server-groups",h.serverGroupOptions)
    r.Get("/api/config/admin/server-groups/{id}/channel-apps",h.serverGroupBindings)
	r.Get("/api/config/nodes/{id}", h.node)
	r.Post("/api/config/nodes/batch-get", h.batch)
	r.Get("/api/config/sync/revision", h.revision)
	r.Get("/api/config/sync/changes", h.changes)
	r.Get("/api/config/sync/snapshot", h.snapshot)
	// 仅当运行实例显式提供写依赖时注册管理写接口。
	if writer != nil {
		r.Post("/api/config/admin/nodes", h.create)
		r.Put("/api/config/admin/nodes/{id}", h.update)
		r.Delete("/api/config/admin/nodes/{id}", h.delete)
		r.Post("/api/config/admin/subscribers", h.createSubscriber)
		r.Post("/api/config/admin/channel-apps",h.createChannelApp)
		r.Put("/api/config/admin/channel-apps/{id}",h.updateChannelApp)
		r.Delete("/api/config/admin/channel-apps/{id}",h.deleteChannelApp)
	}
	return r
}
