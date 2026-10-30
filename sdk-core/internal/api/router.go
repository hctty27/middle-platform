// Package api 提供 SDK Core HTTP 接口。
package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/hctty27/middle-platform/sdk-core/internal/model"
	"github.com/hctty27/middle-platform/shared/logging"
	"github.com/hctty27/middle-platform/sdk-core/internal/mapping"
)

// Fetcher 隔离 HTTP 层与业务实现。
type Fetcher interface {
	Get(context.Context, string, string) (map[string]any, error)
}

type Pinger interface {
	PingContext(context.Context) error
}

// Router 注册不带版本号的 SDK 配置接口和健康检查。
func Router(fetcher Fetcher, db Pinger) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(mapping.RouteCoreConfig, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()

		data, err := fetcher.Get(ctx, r.PathValue("env"), r.PathValue("appid"))
		if err != nil {
			switch {
			case errors.Is(err, model.ErrInvalidRequest):
				writeJSON(w, http.StatusBadRequest, response{Code: 400, Message: "invalid env or appid"})
			case errors.Is(err, model.ErrNotFound):
				writeJSON(w, http.StatusNotFound, response{Code: 404, Message: "channel app not found"})
			case errors.Is(err, model.ErrAmbiguous):
				writeJSON(w, http.StatusConflict, response{Code: 409, Message: "appid is not unique"})
			case errors.Is(err, model.ErrInvalidConfig):
				logging.FromContext(r.Context()).ErrorContext(r.Context(), "invalid sdk configuration", "error", err)
				writeJSON(w, http.StatusInternalServerError, response{Code: 500, Message: "invalid channel app config"})
			default:
				logging.FromContext(r.Context()).ErrorContext(r.Context(), "sdk configuration lookup failed", "error", err)
				writeJSON(w, http.StatusServiceUnavailable, response{Code: 503, Message: "configuration temporarily unavailable"})
			}
			return
		}
		writeJSON(w, http.StatusOK, response{Code: 0, Message: "success", Data: data})
	})
	mux.HandleFunc("GET /health/live", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "UP"})
	})
	mux.HandleFunc("GET /health/ready", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := db.PingContext(ctx); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "DOWN"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "UP"})
	})
	return mux
}

type response struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	body, err := json.Marshal(payload)
	if err != nil {
		http.Error(w, "JSON serialization failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(append(body, '\n'))
}
