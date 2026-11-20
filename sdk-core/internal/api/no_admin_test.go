package api

import (
 "net/http"
 "net/http/httptest"
 "testing"
)

func TestCoreDoesNotExposeAdminRoutes(t *testing.T) {
 router := Router(nil,nil)
 for _, path := range []string{"/api/core/admin/services","/api/core/admin/orders","/api/sdk/admin/services"} {
  rec := httptest.NewRecorder()
  router.ServeHTTP(rec,httptest.NewRequest(http.MethodGet,path,nil))
  if rec.Code!=http.StatusNotFound { t.Fatalf("admin path exposed by SDK Core: %s (%d)",path,rec.Code) }
 }
}
