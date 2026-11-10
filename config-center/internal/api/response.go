package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/go-chi/chi/v5"
	"io"
	"log/slog"
	"net/http"
	"strconv"
)

// writeJSON 统一输出 JSON 响应。
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		slog.Error("编码 JSON 响应失败", "error", err)
	}
}

// writeError 返回兼容旧客户端的错误响应。
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

// writeCodedError 返回包含稳定业务错误码的响应。
func writeCodedError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]string{"code": code, "error": message})
}

// respond 根据业务错误选择相应 HTTP 状态码。
func respond(w http.ResponseWriter, value any, err error) {
	if err == nil {
		writeJSON(w, http.StatusOK, value)
		return
	}
	switch {
	case errors.Is(err, ErrInvalid):
		writeCodedError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "请求参数不合法")
	case errors.Is(err, ErrSubscriberNotFound):
		writeCodedError(w, http.StatusNotFound, "SUBSCRIBER_NOT_FOUND", "订阅不存在")
	case errors.Is(err, sql.ErrNoRows):
		writeCodedError(w, http.StatusNotFound, "RESOURCE_NOT_FOUND", "节点不存在")
	case errors.Is(err, ErrDuplicateCode):
		writeCodedError(w, http.StatusConflict, "DUPLICATE_CODE", ErrDuplicateCode.Error())
	case errors.Is(err, ErrDuplicateSecret):
		writeCodedError(w, http.StatusConflict, "DUPLICATE_SECRET", ErrDuplicateSecret.Error())
	case errors.Is(err, ErrDuplicateValue):
		writeCodedError(w, http.StatusConflict, "DUPLICATE_VALUE", ErrDuplicateValue.Error())
	case errors.Is(err, ErrGameHasRegions):
		writeCodedError(w, http.StatusConflict, "GAME_HAS_REGIONS", ErrGameHasRegions.Error())
	case errors.Is(err, ErrGameHasChannelApps):
		writeCodedError(w, http.StatusConflict, "GAME_HAS_CHANNEL_APPS", ErrGameHasChannelApps.Error())
	case errors.Is(err, ErrRegionHasServerGroups):
		writeCodedError(w, http.StatusConflict, "REGION_HAS_SERVER_GROUPS", ErrRegionHasServerGroups.Error())
	case errors.Is(err, ErrServerGroupInUse):
		writeCodedError(w, http.StatusConflict, "SERVER_GROUP_IN_USE", ErrServerGroupInUse.Error())
	case errors.Is(err, ErrConflict):
		writeCodedError(w, http.StatusConflict, "CONFLICT", "数据存在冲突，请检查后重试")
	default:
		slog.Error("配置查询失败", "error", err)
		writeCodedError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "服务器内部错误")
	}
}

// number 解析整数并应用默认值。
func number(s string, fallback int64) (int64, error) {
	if s == "" {
		return fallback, nil
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, ErrInvalid
	}
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
	if err := decoder.Decode(target); err != nil {
		return ErrInvalid
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return ErrInvalid
	}
	return nil
}
