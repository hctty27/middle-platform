package api

import (
 "net/http"
 "net/http/httptest"
 "strconv"
 "time"
 "hitall.com/middle-platform/shared/signature"
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

func TestAdminSignedSchemaRoute(t *testing.T) {
 const secret = "0123456789abcdef0123456789abcdef"
 path := "/api/sdk/admin/config-schema/region"
 req := httptest.NewRequest(http.MethodGet,path,nil)
 stamp := strconv.FormatInt(time.Now().Unix(),10)
 nonce := "0000000000000001"
 req.Header.Set("X-Timestamp",stamp)
 req.Header.Set("X-Nonce",nonce)
 req.Header.Set("X-Signature",signature.SignAdmin(secret,http.MethodGet,path,"",stamp,nonce,nil))
 recorder := httptest.NewRecorder()
 Router(nil,secret).ServeHTTP(recorder,req)
 if recorder.Code != http.StatusOK { t.Fatalf("signed schema route: got %d: %s",recorder.Code,recorder.Body.String()) }
}
