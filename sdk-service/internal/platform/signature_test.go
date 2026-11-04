package platform

import (
    "net/http/httptest"
    "testing"
    "time"

    "github.com/hctty27/middle-platform/shared/signature"
)

func TestVerifyRequest(t *testing.T) {
    now := time.Unix(1793800000, 0)
    request := httptest.NewRequest("POST", "/api/service/user/check/prod", nil)
    request.Header.Set("x-app-id", "110006")
    request.Header.Set("x-timestamp", "1793800000")
    request.Header.Set("x-nonce", "0011223344556677")
    request.Header.Set("x-signature", signature.SignValues("secret", "110006", "1793800000", "0011223344556677", "user123"))
    if err := VerifyRequest(request, "110006", "secret", "user123", now); err != nil {
        t.Fatalf("有效平台签名应通过: %v", err)
    }
    if err := VerifyRequest(request, "110006", "secret", "other", now); err == nil {
        t.Fatal("修改 open_id 后仍通过签名")
    }
    if err := VerifyRequest(request, "110006", "secret", "user123", now.Add(6*time.Minute)); err == nil {
        t.Fatal("过期时间戳仍通过签名")
    }
}
