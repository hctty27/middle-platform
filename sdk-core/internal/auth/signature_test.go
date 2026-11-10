package auth_test

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"hitall.com/middle-platform/sdk-core/internal/auth"
	"hitall.com/middle-platform/shared/signature"
)

func TestVerifyRegistrationValidatesSignedFieldsAndRequestHeaders(t *testing.T) {
	secret := []byte("secret")
	body := []byte(`{"game_order_id":"G100","amount":600,"attach":"roleId=1"}`)
	stamp := strconv.FormatInt(time.Now().Unix(), 10)
	nonce := "0123456789abcdef"
	req := httptest.NewRequest(http.MethodPost, "/sdk/api/core/test/110006/orders", nil)
	signed := signature.SignGameRequest(secret, req.Method, req.URL.EscapedPath(), "110006", stamp, nonce, body)
	req.SetPathValue("env", "test")
	req.SetPathValue("appid", "110006")
	req.Header.Set("x-app-id", "110006")
	req.Header.Set("x-timestamp", stamp)
	req.Header.Set("x-nonce", nonce)
	req.Header.Set("x-signature", signed)
	verify := func(raw []byte) error { return auth.VerifyRegistration(req, raw, secret) }
	if err := verify(body); err != nil {
		t.Fatal(err)
	}
	modified := []byte(`{"game_order_id":"G100","amount":700,"attach":"roleId=1"}`)
	if err := verify(modified); err == nil {
		t.Fatal("tampered amount accepted")
	}
	req.URL.Path = "/sdk/api/core/prod/110006/orders"
	req.SetPathValue("env", "prod")
	if err := verify(body); err == nil {
		t.Fatal("changed path accepted")
	}
	req.URL.Path = "/sdk/api/core/test/110006/orders"
	req.SetPathValue("env", "test")
	req.SetPathValue("appid", "another")
	if err := verify(body); err == nil {
		t.Fatal("cross-app replay accepted")
	}
	req.SetPathValue("appid", "110006")
	req.Header.Set("x-timestamp", strconv.FormatInt(time.Now().Add(-10*time.Minute).Unix(), 10))
	if err := verify(body); err == nil {
		t.Fatal("expired request accepted")
	}
	req.Header.Set("x-timestamp", stamp)
	req.Header.Set("x-nonce", "bad")
	if err := verify(body); err == nil {
		t.Fatal("bad nonce accepted")
	}
	req.Header.Set("x-nonce", nonce)
	req.Header.Set("x-signature", "BAD")
	if err := verify(body); err == nil {
		t.Fatal("bad signature format accepted")
	}
	req.Header.Set("x-signature", signed)
	req.Header.Del("x-app-id")
	if err := verify(body); err == nil {
		t.Fatal("missing appid header accepted")
	}
	// 代理重写路径会改变签名输入。
	other := httptest.NewRequest(http.MethodPost, "/api/core/test/110006/orders", nil)
	other.SetPathValue("env", "test")
	other.SetPathValue("appid", "110006")
	other.Header = req.Header.Clone()
	other.Header.Set("x-app-id", "110006")
	if err := auth.VerifyRegistration(other, body, secret); err == nil {
		t.Fatal("rewritten path accepted")
	}
}
