package api

import (
	"net/http"
)

// subscribers 查询回调订阅者。
func (h *api) subscribers(w http.ResponseWriter, r *http.Request) {
	out, err := h.reader.Subscribers(r.Context())
	respond(w, out, err)
}

// createSubscriber 注册新的回调订阅者。
func (h *api) createSubscriber(w http.ResponseWriter, r *http.Request) {
	var input CreateSubscriberRequest
	if err := decodeBody(w, r, &input); err != nil { respond(w, nil, err); return }
	if err := h.writer.CreateSubscriber(r.Context(), input); err != nil { respond(w, nil, err); return }
	w.WriteHeader(http.StatusCreated)
}
