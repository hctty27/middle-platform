// Package api 提供 SDK Core HTTP 接口。
package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"hitall.com/middle-platform/sdk-core/internal/model"
	"hitall.com/middle-platform/shared/logging"
)

// Fetcher 提供公开 SDK 配置查询，不暴露原始密钥。
type Fetcher interface {
	Get(context.Context, string, string) (model.ConfigResponse, error)
}

// Pinger 提供依赖数据库的就绪探测。
type Pinger interface{ PingContext(context.Context) error }

// Router 仅提供 SDK 核心配置、订单和充值相关接口。
func Router(fetcher Fetcher, db Pinger, orders ...OrderAPI) http.Handler {
	return router(fetcher, db, nil, RouterOptions{}, orders...)
}

// RouterWithAdmin 额外注册受网关保护的 SDK 管理接口。
func RouterWithAdmin(fetcher Fetcher, db *sql.DB, orders ...OrderAPI) http.Handler {
	return router(fetcher, db, db, RouterOptions{}, orders...)
}

func router(fetcher Fetcher, db Pinger, admin *sql.DB, options RouterOptions, orders ...OrderAPI) http.Handler {
	mux := http.NewServeMux()
	if admin != nil {
		registerAdminRoutes(mux, admin)
		registerAdminOrderRoutes(mux, admin)
	}
	registerWithTimeout(mux, "GET /api/core/config/{env}/{appid}", options.RequestTimeout, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		var body string
		var err error
		env, appID := r.PathValue("env"), r.PathValue("appid")
		if jsonFetcher, ok := fetcher.(interface {
			GetJSON(context.Context, string, string) (string, error)
		}); ok {
			body, err = jsonFetcher.GetJSON(ctx, env, appID)
		} else {
			var data model.ConfigResponse
			data, err = fetcher.Get(ctx, env, appID)
			if err == nil {
				body, err = model.EncodePublicConfig(data)
			}
		}
		if err != nil {
			switch {
			case errors.Is(err, model.ErrInvalidRequest):
				writeJSON(w, http.StatusBadRequest, response{Code: 400, Message: "环境或渠道包 ID 不合法"})
			case errors.Is(err, model.ErrNotFound):
				writeJSON(w, http.StatusNotFound, response{Code: 404, Message: "渠道包不存在或未绑定该环境"})
			case errors.Is(err, model.ErrAmbiguous):
				writeJSON(w, http.StatusConflict, response{Code: 409, Message: "渠道包 ID 不唯一"})
			case errors.Is(err, model.ErrInvalidConfig):
				logging.FromContext(r.Context()).ErrorContext(r.Context(), "SDK 配置不合法", "error", err)
				writeJSON(w, http.StatusInternalServerError, response{Code: 500, Message: "渠道包配置不合法"})
			default:
				logging.FromContext(r.Context()).ErrorContext(r.Context(), "查询 SDK 配置失败", "error", err)
				writeJSON(w, http.StatusServiceUnavailable, response{Code: 503, Message: "配置服务暂时不可用"})
			}
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, body)
	})
	if len(orders) > 0 && orders[0] != nil {
		registerOrderRoutes(mux, orders[0], options)
	}
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
		http.Error(w, "JSON 序列化失败", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(append(body, '\n'))
}
