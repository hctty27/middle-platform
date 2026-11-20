package api

import (
 "net/http"
 "net/http/httptest"
 "testing"
)

func TestAdminRoutesRejectUnsignedRequests(t *testing.T) {
 router := Router(nil, "0123456789abcdef0123456789abcdef")
 for _, path := range []string{
  "/api/sdk/admin/services", "/api/sdk/admin/orders?gameId=1",
  "/api/sdk/admin/node-configs/1", "/api/sdk/admin/config-schema/game",
 } {
  rec := httptest.NewRecorder()
  router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,path,nil))
  if rec.Code != http.StatusUnauthorized { t.Fatalf("%s: want 401, got %d",path,rec.Code) }
 }
 rec := httptest.NewRecorder()
 router.ServeHTTP(rec,httptest.NewRequest(http.MethodGet,"/api/core/admin/services",nil))
 if rec.Code != http.StatusNotFound { t.Fatalf("legacy admin path still active: %d",rec.Code) }
 rec = httptest.NewRecorder()
 router.ServeHTTP(rec,httptest.NewRequest(http.MethodGet,"/health/live",nil))
 if rec.Code != http.StatusOK { t.Fatalf("liveness: %d",rec.Code) }
}
