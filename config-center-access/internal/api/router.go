// Package api 注册接入服务 HTTP 接口和签名回调。
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"time"

	"hitall.com/middle-platform/config-center-access/internal/store"
	"hitall.com/middle-platform/shared/signature"
)

// SyncTrigger 由后台单飞任务实现。
type SyncTrigger interface{ Request() }

// Router 同时提供本地只读查询和仅限可信网络的内部 Webhook。
func Router(local *store.Replica, trigger SyncTrigger, subscriberCode, subscriberSecret string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "UP"})
	})
	mux.HandleFunc("GET /health/ready", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := local.Ready(ctx); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "数据库不可用"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "UP"})
	})
	mux.HandleFunc("GET /api/access/nodes", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		out, err := local.Nodes(ctx)
		respond(w, out, err)
	})
	mux.HandleFunc("GET /api/access/revision", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		out, err := local.Revision(ctx)
		respond(w, out, err)
	})
	mux.HandleFunc("POST /internal/config/callback", func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Epoch    string `json:"epoch"`
			Revision int64  `json:"revision"`
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4096))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "JSON 格式不合法"})
			return
		}
		decoder := json.NewDecoder(bytes.NewReader(body))
		if err := decoder.Decode(&payload); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "JSON 格式不合法"})
			return
		}
		var extra any
		if err := decoder.Decode(&extra); err != io.EOF {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "JSON 格式不合法"})
			return
		}
		if payload.Epoch == "" || payload.Revision < 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "同步回调版本参数不合法"})
			return
		}
		trigger.Request()
		w.WriteHeader(http.StatusAccepted)
	})
	lookup := func(_ context.Context, code string) (string, bool, error) {
		return subscriberSecret, code == subscriberCode, nil
	}
	// 只读接口与内部回调共用同一套 HMAC 验签及 nonce 防重放存储。
	return signature.RequestSubscriberHMACMiddlewareWithNonceStore(
		mux,
		[]string{"/api/access/nodes", "/api/access/revision", "/internal/config/callback"},
		lookup,
		signature.DefaultNonceStore(),
	)
}

// writeJSON 统一接入侧的 JSON 返回格式。
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		slog.Error("写入 JSON 响应失败", "error", err)
	}
}

func respond(w http.ResponseWriter, result any, err error) {
	if err != nil {
		slog.Error("读取本地配置副本失败", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "本地配置副本不可用"})
		return
	}
	writeJSON(w, http.StatusOK, result)
}
