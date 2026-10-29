package config

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

type Reader interface {
	Ready(context.Context) error
	Nodes(context.Context) ([]Node, error)
	Node(context.Context, int64) (Node, error)
	Batch(context.Context, []int64) ([]Node, error)
	Tree(context.Context, int64) ([]NodeSummary, error)
	Children(context.Context, int64, int, int, string) (NodePage, error)
	Search(context.Context, string, int) ([]NodeMatch, error)
	Path(context.Context, int64) (NodeMatch, error)
	Revision(context.Context) (Revision, error)
	Changes(context.Context, int64, int) ([]ChangeEvent, error)
	Snapshot(context.Context) (Snapshot, error)
	Subscribers(context.Context) ([]Subscriber, error)
}

type api struct{ reader Reader }

func Router(reader Reader) http.Handler {
	h := &api{reader: reader}
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
	r.Get("/api/config/admin/nodes", h.list)
	r.Get("/api/config/admin/nodes/tree", h.tree)
	r.Get("/api/config/admin/nodes/children", h.children)
	r.Get("/api/config/admin/nodes/search", h.search)
	r.Get("/api/config/admin/nodes/{id}/children", h.childrenByID)
	r.Get("/api/config/admin/nodes/{id}/path", h.path)
	r.Get("/api/config/admin/nodes/{id}", h.node)
	r.Get("/api/config/admin/subscribers", h.subscribers)
	r.Get("/api/config/nodes/{id}", h.node)
	r.Post("/api/config/nodes/batch-get", h.batch)
	r.Get("/api/config/sync/revision", h.revision)
	r.Get("/api/config/sync/changes", h.changes)
	r.Get("/api/config/sync/snapshot", h.snapshot)
	return r
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		slog.Error("encode JSON response failed", "error", err)
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func respond(w http.ResponseWriter, value any, err error) {
	if err == nil { writeJSON(w, http.StatusOK, value); return }
	switch {
	case errors.Is(err, ErrInvalid):
		writeError(w, http.StatusBadRequest, "invalid request")
	case errors.Is(err, sql.ErrNoRows):
		writeError(w, http.StatusNotFound, "node not found")
	default:
		slog.Error("configuration query failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
	}
}

func number(s string, fallback int64) (int64, error) {
	if s == "" { return fallback, nil }
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil { return 0, ErrInvalid }
	return n, nil
}

func param(r *http.Request, name string, fallback int64) (int64, error) {
	return number(r.URL.Query().Get(name), fallback)
}

func id(r *http.Request) (int64, error) {
	return number(chi.URLParam(r, "id"), -1)
}

func (h *api) list(w http.ResponseWriter, r *http.Request) {
	out, err := h.reader.Nodes(r.Context())
	respond(w, out, err)
}

func (h *api) node(w http.ResponseWriter, r *http.Request) {
	n, err := id(r)
	if err != nil { respond(w, nil, err); return }
	out, err := h.reader.Node(r.Context(), n)
	respond(w, out, err)
}

func (h *api) tree(w http.ResponseWriter, r *http.Request) {
	n, err := param(r, "parentId", 0)
	if err != nil { respond(w, nil, err); return }
	out, err := h.reader.Tree(r.Context(), n)
	respond(w, out, err)
}

func (h *api) childrenByID(w http.ResponseWriter, r *http.Request) {
	n, err := id(r)
	if err != nil { respond(w, nil, err); return }
	h.page(w, r, n)
}

func (h *api) children(w http.ResponseWriter, r *http.Request) {
	if _, ok := r.URL.Query()["parentId"]; !ok { respond(w, nil, ErrInvalid); return }
	n, err := param(r, "parentId", -1)
	if err != nil { respond(w, nil, err); return }
	h.page(w, r, n)
}

func (h *api) page(w http.ResponseWriter, r *http.Request, parent int64) {
	p, err := param(r, "page", 1)
	if err != nil { respond(w, nil, err); return }
	size, err := param(r, "size", 20)
	if err != nil { respond(w, nil, err); return }
	if p < 1 || p > 1_000_000_000 || size < 1 || size > 100 { respond(w, nil, ErrInvalid); return }
	out, err := h.reader.Children(r.Context(), parent, int(p), int(size), r.URL.Query().Get("keyword"))
	respond(w, out, err)
}

func (h *api) search(w http.ResponseWriter, r *http.Request) {
	if _, ok := r.URL.Query()["keyword"]; !ok { respond(w, nil, ErrInvalid); return }
	limit, err := param(r, "limit", 30)
	if err != nil { respond(w, nil, err); return }
	if limit < 1 || limit > 50 { respond(w, nil, ErrInvalid); return }
	out, err := h.reader.Search(r.Context(), r.URL.Query().Get("keyword"), int(limit))
	respond(w, out, err)
}

func (h *api) path(w http.ResponseWriter, r *http.Request) {
	n, err := id(r)
	if err != nil { respond(w, nil, err); return }
	out, err := h.reader.Path(r.Context(), n)
	respond(w, out, err)
}

func (h *api) batch(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var req struct { IDs []int64 `json:"ids"` }
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	if err := dec.Decode(&req); err != nil { respond(w, nil, ErrInvalid); return }
	if len(req.IDs) > 500 { respond(w, nil, ErrInvalid); return }
	out, err := h.reader.Batch(r.Context(), req.IDs)
	respond(w, out, err)
}

func (h *api) revision(w http.ResponseWriter, r *http.Request) {
	out, err := h.reader.Revision(r.Context())
	respond(w, out, err)
}

func (h *api) changes(w http.ResponseWriter, r *http.Request) {
	after, err := param(r, "afterRevision", 0)
	if err != nil { respond(w, nil, err); return }
	limit, err := param(r, "limit", 200)
	if err != nil { respond(w, nil, err); return }
	if after < 0 || limit < 1 || limit > 500 { respond(w, nil, ErrInvalid); return }
	out, err := h.reader.Changes(r.Context(), after, int(limit))
	respond(w, out, err)
}

func (h *api) snapshot(w http.ResponseWriter, r *http.Request) {
	out, err := h.reader.Snapshot(r.Context())
	respond(w, out, err)
}

func (h *api) subscribers(w http.ResponseWriter, r *http.Request) {
	out, err := h.reader.Subscribers(r.Context())
	respond(w, out, err)
}

