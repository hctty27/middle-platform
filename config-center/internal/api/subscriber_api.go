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
	// 密钥在创建时写入，不从订阅者查询接口向管理前端回显。
	var input CreateSubscriberRequest
	if err := decodeBody(w, r, &input); err != nil {
		respond(w, nil, err)
		return
	}
	if err := h.writer.CreateSubscriber(r.Context(), input); err != nil {
		respond(w, nil, err)
		return
	}
	w.WriteHeader(http.StatusCreated)
}


// updateSubscriber 修改订阅者回调地址及可选签名密钥。
func (h *api) updateSubscriber(w http.ResponseWriter, r *http.Request) {
	subscriberID, err := id(r)
	if err != nil {
		respond(w, nil, err)
		return
	}
	var input UpdateSubscriberRequest
	if err := decodeBody(w, r, &input); err != nil {
		respond(w, nil, err)
		return
	}
	if err := h.writer.UpdateSubscriber(r.Context(), subscriberID, input); err != nil {
		respond(w, nil, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// setSubscriberEnabled 单独修改订阅者启用状态。
func (h *api) setSubscriberEnabled(w http.ResponseWriter, r *http.Request) {
	subscriberID, err := id(r)
	if err != nil {
		respond(w, nil, err)
		return
	}
	var input SetSubscriberEnabledRequest
	if err := decodeBody(w, r, &input); err != nil || input.Enabled == nil {
		respond(w, nil, ErrInvalid)
		return
	}
	if err := h.writer.SetSubscriberEnabled(r.Context(), subscriberID, *input.Enabled); err != nil {
		respond(w, nil, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// deleteSubscriber 删除订阅者及其关联投递状态。
func (h *api) deleteSubscriber(w http.ResponseWriter, r *http.Request) {
	subscriberID, err := id(r)
	if err != nil {
		respond(w, nil, err)
		return
	}
	if err := h.writer.DeleteSubscriber(r.Context(), subscriberID); err != nil {
		respond(w, nil, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
