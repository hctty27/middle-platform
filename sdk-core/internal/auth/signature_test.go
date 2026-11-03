package auth_test

import (
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hctty27/middle-platform/sdk-core/internal/auth"
)

func referenceSignature(t *testing.T, master []byte, purpose, canonical string) string {
	t.Helper()
	key, err := hkdf.Key(sha256.New, master, nil,
		"middle-platform:sdk-core:"+purpose+":v2", 32)
	if err != nil {
		t.Fatal(err)
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(canonical))
	return hex.EncodeToString(mac.Sum(nil))
}

func TestRegistrationSignatureV2MatchesIndependentVector(t *testing.T) {
	secret := []byte("自定义密钥-123!@#")
	body := []byte(`{"game_order_id":"G100","amount":600,"attach":"roleId=1"}`)
	timestamp := "1790000000"
	hash := sha256.Sum256(body)
	canonical := fmt.Sprintf("POST\ntest\n110006\n%s\n%x", timestamp, hash)
	expected := referenceSignature(t, secret, auth.PurposeOrderRegister, canonical)

	actual, err := auth.SignRegistration(secret, "test", "110006", timestamp, body)
	if err != nil {
		t.Fatal(err)
	}
	if actual != expected {
		t.Fatalf("V2 registration signature mismatch: got %s, want %s", actual, expected)
	}

	req := httptest.NewRequest("POST", "/any/proxy/path", nil)
	req.SetPathValue("env", "test")
	req.SetPathValue("appid", "110006")
	req.Header.Set("X-Game-Timestamp", fmt.Sprint(time.Now().Unix()))
	current, err := auth.SignRegistration(secret, "test", "110006", req.Header.Get("X-Game-Timestamp"), body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Game-Signature", current)
	if err := auth.VerifyRegistration(req, body, secret); err != nil {
		t.Fatalf("valid signature rejected: %v", err)
	}
	req.SetPathValue("appid", "110007")
	if err := auth.VerifyRegistration(req, body, secret); err == nil {
		t.Fatal("cross-app signature replay accepted")
	}
}

func TestNotificationSignatureV2MatchesIndependentVector(t *testing.T) {
	secret := []byte("自定义密钥-123!@#")
	key, err := auth.Derive(secret, auth.PurposePaymentNotify)
	if err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"order_no":"P100","game_order_id":"G100","amount":600,"attach":"","paid_at":"2026-11-03T00:00:00Z"}`)
	timestamp, nonce := "1790000000", "0123456789abcdef"
	hash := sha256.Sum256(body)
	canonical := fmt.Sprintf("110006\nPOST\n%s\n%s\n%x", timestamp, nonce, hash)
	expected := referenceSignature(t, secret, auth.PurposePaymentNotify, canonical)

	actual, err := auth.SignNotification(key, "110006", timestamp, nonce, body)
	if err != nil {
		t.Fatal(err)
	}
	if actual != expected {
		t.Fatalf("V2 notification signature mismatch: got %s, want %s", actual, expected)
	}

	if _, err := auth.SignNotification([]byte("short"), "110006", timestamp, nonce, body); err == nil {
		t.Fatal("accepted invalid derived key length")
	}
	if _, err := auth.SignRegistration(nil, "test", "110006", timestamp, body); err == nil {
		t.Fatal("accepted empty game secret")
	}
}
