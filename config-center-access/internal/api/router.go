// Package api 注册接入服务 HTTP 接口和签名回调。
package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/hctty27/middle-platform/config-center-access/internal/store"
)

// SyncTrigger 由后台单飞任务实现。
type SyncTrigger interface{ Request() }

// Router 同时提供本地只读查询和仅限可信网络的内部 Webhook。
func Router(local *store.Replica, trigger SyncTrigger, secret string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status":"UP"})
	})
	mux.HandleFunc("GET /health/ready", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := local.Ready(ctx); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error":"database unavailable"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status":"UP"})
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
			Epoch string `json:"epoch"`
			Revision int64 `json:"revision"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		if err := decoder.Decode(&payload); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error":"invalid JSON"})
			return
		}
		var extra any
		if err := decoder.Decode(&extra); err != io.EOF {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error":"invalid JSON"})
			return
		}
		if !verify(r.Header.Get("X-Config-Timestamp"), r.Header.Get("X-Config-Signature"), payload.Epoch, payload.Revision, secret) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error":"invalid callback signature"})
			return
		}
		trigger.Request()
		w.WriteHeader(http.StatusAccepted)
	})
	return mux
}

// verify 校验签名和五分钟时间窗口，拒绝篡改或重放的同步通知。
func verify(timestamp, signature, epoch string, revision int64, secret string) bool {
	ts, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil || secret == "" || epoch == "" || revision < 0 { return false }
	now := time.Now().Unix()
	if ts < now-300 || ts > now+300 { return false }
	if !strings.HasPrefix(signature, "sha256=") { return false }
	digest, err := hex.DecodeString(strings.TrimPrefix(signature, "sha256="))
	if err != nil || len(digest) != sha256.Size { return false }
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = io.WriteString(mac, timestamp+"."+epoch+"."+strconv.FormatInt(revision, 10))
	return hmac.Equal(digest, mac.Sum(nil))
}

// writeJSON 统一接入侧的 JSON 返回格式。
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		slog.Error("write JSON failed", "error", err)
	}
}

func respond(w http.ResponseWriter, result any, err error) {
	if err != nil {
		slog.Error("read replica failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error":"replica unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, result)
}
