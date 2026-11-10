// Package api 注册接入服务 HTTP 接口和签名回调。
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"hitall.com/middle-platform/config-center-access/internal/store"
	"hitall.com/middle-platform/shared/signature"
)

// SyncTrigger 由后台单飞任务实现。
type SyncTrigger interface{ Request() }

// Router 同时提供本地只读查询和仅限可信网络的内部 Webhook。
func Router(local *store.Replica, trigger SyncTrigger, subscriberCode, subscriberSecret string) http.Handler {
	mux := http.NewServeMux()
	nonces := &nonceCache{used: make(map[string]time.Time)}
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
		if r.Header.Get("X-Subscriber-Code") != subscriberCode || !signature.VerifySubscriberHMACRequest(r, subscriberSecret, body, time.Now()) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "同步回调 HMAC-SHA256 签名验证失败"})
			return
		}
		nonce := r.Header.Get("X-Nonce")
		if !nonces.accept(nonce, time.Now()) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "同步回调 nonce 已使用或请求过于频繁"})
			return
		}
		trigger.Request()
		w.WriteHeader(http.StatusAccepted)
	})
	return signature.RequestSubscriberHMACMiddleware(mux, []string{"/api/access/nodes", "/api/access/revision"}, func(_ context.Context, code string) (string, bool, error) {
		if code != subscriberCode {
			return "", false, nil
		}
		return subscriberSecret, true, nil
	})
}

// nonceCache 仅对当前进程中的签名请求进行去重。
// epoch/revision 仍由同步逻辑幂等处理；多实例全局去重需共享存储。
type nonceCache struct {
	mu   sync.Mutex
	used map[string]time.Time
}

func (c *nonceCache) accept(nonce string, now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if until, ok := c.used[nonce]; ok && until.After(now) {
		return false
	}
	if len(c.used) >= 10000 {
		for key, until := range c.used {
			if !until.After(now) {
				delete(c.used, key)
			}
		}
	}
	if len(c.used) >= 10000 {
		return false
	}
	c.used[nonce] = now.Add(10 * time.Minute)
	return true
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
