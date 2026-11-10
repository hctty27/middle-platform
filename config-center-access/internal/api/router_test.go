package api

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"hitall.com/middle-platform/shared/signature"
)

const testAccessSyncSecret = "config-sync-secret-0123456789abcdef0123456789"

type fakeTrigger struct{ count atomic.Int64 }

func (f *fakeTrigger) Request() { f.count.Add(1) }

func signedCallback(secret, timestamp, nonce string, body []byte) string {
	req := httptest.NewRequest(http.MethodPost, "/internal/config/callback", nil)
	return signature.SignHMACRequest(secret, req.Method, req.URL.EscapedPath(), "", timestamp, nonce, body)
}

func TestCallbackHMACSignatureAndAcceptance(t *testing.T) {
	trigger := &fakeTrigger{}
	const secret = "test-webhook-secret"
	handler := Router(nil, trigger, secret, testAccessSyncSecret)
	body := []byte(`{"epoch":"an-epoch","revision":42}`)
	timestamp := fmt.Sprint(time.Now().Unix())
	nonce := "0123456789abcdef"
	signed := signedCallback(secret, timestamp, nonce, body)
	freshNonce := "fedcba9876543210"
	fresh := signedCallback(secret, timestamp, freshNonce, body)
	tamperedBodySignature := signedCallback(secret, timestamp, "0011223344556677", body)
	expiredTimestamp := fmt.Sprint(time.Now().Add(-10 * time.Minute).Unix())
	expired := signedCallback(secret, expiredTimestamp, "0011223344556677", body)
	tests := []struct {
		name                    string
		body                    []byte
		timestamp, nonce, signed string
		want                    int
	}{
		{"valid", body, timestamp, nonce, signed, http.StatusAccepted},
		{"replay", body, timestamp, nonce, signed, http.StatusConflict},
		{"invalid", body, timestamp, "0011223344556677", "bad", http.StatusUnauthorized},
		{"old-sha1", body, timestamp, "0011223344556677", "a95aee794bee55d020586fd60c8b0fe860779636", http.StatusUnauthorized},
		{"expired", body, expiredTimestamp, "0011223344556677", expired, http.StatusUnauthorized},
		{"missing-nonce", body, timestamp, "", signed, http.StatusUnauthorized},
		{"tampered-body", []byte(`{"epoch":"changed","revision":42}`), timestamp, "0011223344556677", tamperedBodySignature, http.StatusUnauthorized},
		{"fresh-retry", body, timestamp, freshNonce, fresh, http.StatusAccepted},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/internal/config/callback", bytes.NewReader(tc.body))
			req.Header.Set("X-Timestamp", tc.timestamp)
			req.Header.Set("X-Nonce", tc.nonce)
			req.Header.Set("X-Signature", tc.signed)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			if w.Code != tc.want {
				t.Fatalf("HTTP %d，期望 %d: %s", w.Code, tc.want, w.Body.String())
			}
		})
	}
	if got := trigger.count.Load(); got != 2 {
		t.Fatalf("触发 %d 次，期望 2 次", got)
	}
}

func TestConfigWebhookHMACSignatureFixture(t *testing.T) {
	body := []byte(`{"epoch":"550e8400-e29b-41d4-a716-446655440000","revision":12}`)
	got := signedCallback("demo-webhook-secret", "1793865600", "0123456789abcdef", body)
	if want := "a8673ae1c4b85d5788ca1f9c00f0a22799e023ca492073c3b574d8e07a4f9846"; got != want {
		t.Fatalf("HMAC-SHA256 签名向量错误: got %s, want %s", got, want)
	}
}

func TestCallbackRejectsMalformedJSON(t *testing.T) {
	trigger := &fakeTrigger{}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/internal/config/callback", bytes.NewBufferString("{bad"))
	Router(nil, trigger, "secret", testAccessSyncSecret).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest || trigger.count.Load() != 0 {
		t.Fatal(w.Code)
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("JSON 格式不合法")) {
		t.Fatal(w.Body.String())
	}
}

func TestAccessReadEndpointsRequireHMAC(t *testing.T) {
	handler := Router(nil, &fakeTrigger{}, "webhook-secret", testAccessSyncSecret)
	for _, path := range []string{"/api/access/nodes", "/api/access/revision"} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("unsigned access request %s returned HTTP %d", path, rec.Code)
		}
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health/live", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("health endpoint should remain public: %d", rec.Code)
	}
}
