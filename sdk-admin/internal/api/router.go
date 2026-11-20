// Package api 提供 SDK 管理接口，独立于高并发 SDK Core 业务。
package api

import (
 "context"
 "database/sql"
 "encoding/json"
 "net/http"
 "time"

 "hitall.com/middle-platform/shared/signature"
)

type response struct {
 Code int `json:"code"`
 Message string `json:"message"`
 Data any `json:"data,omitempty"`
}
func writeJSON(w http.ResponseWriter, status int, payload any) {
 body, err := json.Marshal(payload)
 if err != nil { http.Error(w, "JSON 序列化失败", http.StatusInternalServerError); return }
 w.Header().Set("Content-Type", "application/json; charset=utf-8")
 w.WriteHeader(status)
 _, _ = w.Write(append(body, '\n'))
}

// Router 对全部管理业务路由强制 HMAC 验签；健康检查无需管理签名。
func Router(db *sql.DB, secret string) http.Handler {
 mux := http.NewServeMux()
 registerAdminRoutes(mux, db)
 registerAdminOrderRoutes(mux, db)
 mux.HandleFunc("GET /health/live", func(w http.ResponseWriter, _ *http.Request) {
  writeJSON(w, 200, map[string]string{"status":"UP"})
 })
 mux.HandleFunc("GET /health/ready", func(w http.ResponseWriter, r *http.Request) {
  ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
  defer cancel()
  if db == nil || db.PingContext(ctx) != nil {
   writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status":"DOWN"}); return
  }
  writeJSON(w, 200, map[string]string{"status":"UP"})
 })
 return signature.AdminMiddleware(mux, "/api/sdk/admin", secret)
}
