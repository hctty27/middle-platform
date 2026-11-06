package api

import (
    "net/http"
    "github.com/hctty27/middle-platform/sdk-core/internal/configschema"
)

// getConfigSchema 供 SDK 管理页面和第三方接入方发现配置字段。
func getConfigSchema(w http.ResponseWriter, r *http.Request) {
    schema, ok := configschema.Schema(r.PathValue("nodeType"))
    if !ok { adminError(w, http.StatusNotFound, "不支持的节点类型"); return }
    w.Header().Set("Cache-Control", "no-store")
    writeJSON(w, http.StatusOK, schema)
}
