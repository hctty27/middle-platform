package api

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"hitall.com/middle-platform/shared/signature"
)

const (
	testAccessSubscriberCode = "access-test"
	testAccessSyncSecret = "config-sync-secret-0123456789abcdef0123456789"
)

type fakeTrigger struct{ count atomic.Int64 }

func (f *fakeTrigger) Request() { f.count.Add(1) }

func signedCallback(code, secret, timestamp, nonce string, body []byte) string {
	req := httptest.NewRequest(http.MethodPost, "/internal/config/callback", nil)
	return signature.SignSubscriberHMACRequest(secret, code, req.Method, req.URL.EscapedPath(), "", timestamp, nonce, body)
}

func TestCallbackHMACSignatureAndAcceptance(t *testing.T) {
	trigger := &fakeTrigger{}
	handler := Router(nil, trigger, testAccessSubscriberCode, testAccessSyncSecret)
	body := []byte(`{"epoch":"an-epoch","revision":42}`)
	timestamp := fmt.Sprint(time.Now().Unix())
	nonce := "0123456789abcdef"
	signed := signedCallback(testAccessSubscriberCode, testAccessSyncSecret, timestamp, nonce, body)
	freshNonce := "fedcba9876543210"
	fresh := signedCallback(testAccessSubscriberCode, testAccessSyncSecret, timestamp, freshNonce, body)
	tamperedBodySignature := signedCallback(testAccessSubscriberCode, testAccessSyncSecret, timestamp, "0011223344556677", body)
	expiredTimestamp := fmt.Sprint(time.Now().Add(-10 * time.Minute).Unix())
	expired := signedCallback(testAccessSubscriberCode, testAccessSyncSecret, expiredTimestamp, "0011223344556677", body)
	wrongIdentity := signedCallback("other-access", testAccessSyncSecret, timestamp, "1021324354657687", body)
	tests := []struct {
		name                     string
		body                     []byte
		code                     string
		timestamp, nonce, signed string
		want                     int
	}{
		{"valid", body, testAccessSubscriberCode, timestamp, nonce, signed, http.StatusAccepted},
		{"replay", body, testAccessSubscriberCode, timestamp, nonce, signed, http.StatusConflict},
		{"invalid", body, testAccessSubscriberCode, timestamp, "0011223344556677", "bad", http.StatusUnauthorized},
		{"old-sha1", body, testAccessSubscriberCode, timestamp, "0011223344556677", "a95aee794bee55d020586fd60c8b0fe860779636", http.StatusUnauthorized},
		{"expired", body, testAccessSubscriberCode, expiredTimestamp, "0011223344556677", expired, http.StatusUnauthorized},
		{"missing-nonce", body, testAccessSubscriberCode, timestamp, "", signed, http.StatusUnauthorized},
		{"tampered-body", []byte(`{"epoch":"changed","revision":42}`), testAccessSubscriberCode, timestamp, "0011223344556677", tamperedBodySignature, http.StatusUnauthorized},
		{"wrong-identity", body, "other-access", timestamp, "1021324354657687", wrongIdentity, http.StatusUnauthorized},
		{"fresh-retry", body, testAccessSubscriberCode, timestamp, freshNonce, fresh, http.StatusAccepted},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/internal/config/callback", bytes.NewReader(tc.body))
			req.Header.Set("X-Subscriber-Code", tc.code)
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
	got := signedCallback(testAccessSubscriberCode, "demo-subscriber-secret-0123456789abcdef", "1793865600", "0123456789abcdef", body)
	if want := "cb00b3b9869452bdea8846fc381d94a569dec6e796e682763a27221129e8c7d5"; got != want {
		t.Fatalf("HMAC-SHA256 签名向量错误: got %s, want %s", got, want)
	}
}

func TestCallbackRejectsMalformedJSON(t *testing.T) {
	trigger := &fakeTrigger{}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/internal/config/callback", bytes.NewBufferString("{bad"))
	Router(nil, trigger, testAccessSubscriberCode, testAccessSyncSecret).ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest || trigger.count.Load() != 0 {
		t.Fatal(w.Code)
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("JSON 格式不合法")) {
		t.Fatal(w.Body.String())
	}
}

func TestAccessReadEndpointsRequireSubscriberHMAC(t *testing.T) {
	handler := Router(nil, &fakeTrigger{}, testAccessSubscriberCode, testAccessSyncSecret)
	for _, path := range []string{"/api/access/nodes", "/api/access/revision"} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("unsigned access request %s returned HTTP %d", path, rec.Code)
		}
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("X-Subscriber-Code", testAccessSubscriberCode)
		req.Header.Set("X-Timestamp", fmt.Sprint(time.Now().Unix()))
		req.Header.Set("X-Nonce", "0123456789abcdef")
		req.Header.Set("X-Signature", signature.SignSubscriberHMACRequest(testAccessSyncSecret, testAccessSubscriberCode, req.Method, req.URL.EscapedPath(), "", req.Header.Get("X-Timestamp"), req.Header.Get("X-Nonce"), nil))
		rec = httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("signed access request should reach local store, got HTTP %d", rec.Code)
		}
	}
	wrong := httptest.NewRequest(http.MethodGet, "/api/access/revision", nil)
	wrong.Header.Set("X-Subscriber-Code", "other-access")
	wrong.Header.Set("X-Timestamp", fmt.Sprint(time.Now().Unix()))
	wrong.Header.Set("X-Nonce", "fedcba9876543210")
	wrong.Header.Set("X-Signature", signature.SignSubscriberHMACRequest(testAccessSyncSecret, "other-access", wrong.Method, wrong.URL.EscapedPath(), "", wrong.Header.Get("X-Timestamp"), wrong.Header.Get("X-Nonce"), nil))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, wrong)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong subscriber code should be rejected: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health/live", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("health endpoint should remain public: %d", rec.Code)
	}
}
