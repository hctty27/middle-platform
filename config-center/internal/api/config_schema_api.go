package api

import (
    "net/http"
    "github.com/go-chi/chi/v5"
    "github.com/hctty27/middle-platform/config-center/internal/configschema"
)

// configSchema 供管理端及第三方读取基础配置字段定义。
func (h *api) configSchema(w http.ResponseWriter, r *http.Request) {
    schema, ok := configschema.Schema(chi.URLParam(r, "nodeType"))
    if !ok { writeError(w, http.StatusNotFound, "不支持的节点类型"); return }
    w.Header().Set("Cache-Control", "no-store")
    writeJSON(w, http.StatusOK, schema)
}
