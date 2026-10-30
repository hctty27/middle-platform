package api

import (
	"encoding/json"
	"net/http"
)

// list 返回完整配置节点列表。
func (h *api) list(w http.ResponseWriter, r *http.Request) {
	out, err := h.reader.Nodes(r.Context())
	respond(w, out, err)
}

// node 返回指定节点。
func (h *api) node(w http.ResponseWriter, r *http.Request) {
	n, err := id(r)
	if err != nil { respond(w, nil, err); return }
	out, err := h.reader.Node(r.Context(), n)
	respond(w, out, err)
}

// tree 返回配置树当前层信息。
func (h *api) tree(w http.ResponseWriter, r *http.Request) {
	n, err := param(r, "parentId", 0)
	if err != nil { respond(w, nil, err); return }
	out, err := h.reader.Tree(r.Context(), n)
	respond(w, out, err)
}

// childrenByID 查询路径参数指定的父节点的子项。
func (h *api) childrenByID(w http.ResponseWriter, r *http.Request) {
	n, err := id(r)
	if err != nil { respond(w, nil, err); return }
	h.page(w, r, n)
}

// children 根据查询参数获取子项。
func (h *api) children(w http.ResponseWriter, r *http.Request) {
	if _, ok := r.URL.Query()["parentId"]; !ok { respond(w, nil, ErrInvalid); return }
	n, err := param(r, "parentId", -1)
	if err != nil { respond(w, nil, err); return }
	h.page(w, r, n)
}

// page 统一分页校验并查询直接子项。
func (h *api) page(w http.ResponseWriter, r *http.Request, parent int64) {
	p, err := param(r, "page", 1)
	if err != nil { respond(w, nil, err); return }
	size, err := param(r, "size", 20)
	if err != nil { respond(w, nil, err); return }
	if p < 1 || p > 1_000_000_000 || size < 1 || size > 100 { respond(w, nil, ErrInvalid); return }
	out, err := h.reader.Children(r.Context(), parent, int(p), int(size), r.URL.Query().Get("keyword"))
	respond(w, out, err)
}

// search 根据关键字查询节点路径。
func (h *api) search(w http.ResponseWriter, r *http.Request) {
	if _, ok := r.URL.Query()["keyword"]; !ok { respond(w, nil, ErrInvalid); return }
	limit, err := param(r, "limit", 30)
	if err != nil { respond(w, nil, err); return }
	if limit < 1 || limit > 50 { respond(w, nil, ErrInvalid); return }
	out, err := h.reader.Search(r.Context(), r.URL.Query().Get("keyword"), int(limit))
	respond(w, out, err)
}

// path 获取节点祖先路径。
func (h *api) path(w http.ResponseWriter, r *http.Request) {
	n, err := id(r)
	if err != nil { respond(w, nil, err); return }
	out, err := h.reader.Path(r.Context(), n)
	respond(w, out, err)
}

// batch 批量获取指定配置节点。
func (h *api) batch(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var req struct { IDs []int64 `json:"ids"` }
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	if err := dec.Decode(&req); err != nil { respond(w, nil, ErrInvalid); return }
	if len(req.IDs) > 500 { respond(w, nil, ErrInvalid); return }
	out, err := h.reader.Batch(r.Context(), req.IDs)
	respond(w, out, err)
}

// create 处理新增配置节点请求。
func (h *api) create(w http.ResponseWriter, r *http.Request) {
	var input CreateNodeRequest
	if err := decodeBody(w, r, &input); err != nil { respond(w, nil, err); return }
	node, err := h.writer.Create(r.Context(), input)
	if err != nil { respond(w, nil, err); return }
	writeJSON(w, http.StatusCreated, node)
}

// update 处理修改配置节点请求。
func (h *api) update(w http.ResponseWriter, r *http.Request) {
	id, err := id(r)
	if err != nil { respond(w, nil, err); return }
	var input UpdateNodeRequest
	if err := decodeBody(w, r, &input); err != nil { respond(w, nil, err); return }
	node, err := h.writer.Update(r.Context(), id, input)
	respond(w, node, err)
}

// delete 处理删除配置节点请求。
func (h *api) delete(w http.ResponseWriter, r *http.Request) {
	id, err := id(r)
	if err != nil { respond(w, nil, err); return }
	if err := h.writer.Delete(r.Context(), id); err != nil { respond(w, nil, err); return }
	w.WriteHeader(http.StatusNoContent)
}
