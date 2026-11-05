package platform

import (
    "net/http/httptest"
    "testing"
    "time"

    "github.com/hctty27/middle-platform/shared/signature"
)

func TestVerifyRoleQueryFields(t *testing.T) {
    now := time.Unix(1793800000, 0)
    request := httptest.NewRequest("POST", "/api/service/role/query/test", nil)
    request.Header.Set("x-app-id", "110006")
    request.Header.Set("x-timestamp", "1793800000")
    request.Header.Set("x-nonce", "0011223344556677")
    fields := []string{"user123", "role100", "", "server1", "extension"}
    values := append([]string{"secret", "110006", "1793800000", "0011223344556677"}, fields...)
    request.Header.Set("x-signature", signature.SignValues(values...))
    if err := VerifyFields(request, "110006", "secret", now, fields...); err != nil {
        t.Fatalf("多字段角色查询签名未通过: %v", err)
    }
    if err := VerifyFields(request, "110006", "secret", now, "user123", "changed"); err == nil {
        t.Fatal("篡改角色字段后验签仍通过")
    }
    if err := VerifyFields(request, "110006", "secret", now.Add(6*time.Minute), fields...); err == nil {
        t.Fatal("过期角色查询签名未拒绝")
    }
}
