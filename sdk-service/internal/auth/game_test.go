package auth

import (
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"hitall.com/middle-platform/shared/signature"
)

func TestGameRequestSignature(t *testing.T) {
	body := []byte(`{"event":"login","ext":{"roleId":"100","roleLevel":80}}`)
	now := time.Now()
	timestamp := strconv.FormatInt(now.Unix(), 10)
	nonce := "0011223344556677"
	req := httptest.NewRequest("POST", "/api/service/events/test", nil)
	req.Header.Set("x-app-id", "110006")
	req.Header.Set("x-timestamp", timestamp)
	req.Header.Set("x-nonce", nonce)
	req.Header.Set("x-signature", signature.SignGameRequest([]byte("game-secret"), req.Method, req.URL.EscapedPath(), "110006", timestamp, nonce, body))
	if err := VerifyGameRequest(req, "110006", "game-secret", body, now); err != nil {
		t.Fatal(err)
	}
	if err := VerifyGameRequest(req, "110006", "game-secret", []byte(`{"event":"logout"}`), now); err == nil {
		t.Fatal("tampered body accepted")
	}
	if err := VerifyGameRequest(req, "110006", "game-secret", body, now.Add(6*time.Minute)); err == nil {
		t.Fatal("expired request accepted")
	}
	if err := VerifyGameRequest(req, "another", "game-secret", body, now); err == nil {
		t.Fatal("wrong app accepted")
	}
}
