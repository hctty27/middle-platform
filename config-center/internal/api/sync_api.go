package api

import (
	"net/http"
)

// revision 返回当前同步版本。
func (h *api) revision(w http.ResponseWriter, r *http.Request) {
	out, err := h.reader.Revision(r.Context())
	respond(w, out, err)
}

// changes 返回指定版本之后的增量事件。
func (h *api) changes(w http.ResponseWriter, r *http.Request) {
	after, err := param(r, "afterRevision", 0)
	if err != nil { respond(w, nil, err); return }
	limit, err := param(r, "limit", 200)
	if err != nil { respond(w, nil, err); return }
	if after < 0 || limit < 1 || limit > 500 { respond(w, nil, ErrInvalid); return }
	out, err := h.reader.Changes(r.Context(), after, int(limit))
	respond(w, out, err)
}

// snapshot 返回完整配置快照。
func (h *api) snapshot(w http.ResponseWriter, r *http.Request) {
	out, err := h.reader.Snapshot(r.Context())
	respond(w, out, err)
}
