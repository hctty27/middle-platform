package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"github.com/go-chi/chi/v5"
)

// writeJSON 统一输出 JSON 响应。
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		slog.Error("encode JSON response failed", "error", err)
	}
}

// writeError 返回稳定的错误响应格式。
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

// respond 根据业务错误选择相应 HTTP 状态码。
func respond(w http.ResponseWriter, value any, err error) {
	if err == nil { writeJSON(w, http.StatusOK, value); return }
	switch {
	case errors.Is(err, ErrInvalid):
		writeError(w, http.StatusBadRequest, "invalid request")
	case errors.Is(err, sql.ErrNoRows):
		writeError(w, http.StatusNotFound, "node not found")
	case errors.Is(err, ErrConflict):
		writeError(w, http.StatusConflict, "conflicting configuration")
	default:
		slog.Error("configuration query failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
	}
}

// number 解析整数并应用默认值。
func number(s string, fallback int64) (int64, error) {
	if s == "" { return fallback, nil }
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil { return 0, ErrInvalid }
	return n, nil
}

// param 读取并校验查询参数。
func param(r *http.Request, name string, fallback int64) (int64, error) {
	return number(r.URL.Query().Get(name), fallback)
}

// id 解析请求路径中的节点 ID。
func id(r *http.Request) (int64, error) {
	return number(chi.URLParam(r, "id"), -1)
}

// decodeBody 限制请求体大小并拒绝多段 JSON。
// decodeBody 对请求体设置大小与 JSON 结构边界，阻止多份 JSON 拼接请求。
func decodeBody(w http.ResponseWriter, r *http.Request, target any) error {
	defer r.Body.Close()
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	if err := decoder.Decode(target); err != nil { return ErrInvalid }
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) { return ErrInvalid }
	return nil
}
