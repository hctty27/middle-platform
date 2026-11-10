package signature

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

const testAdminSecret = "0123456789abcdef0123456789abcdef"

func signedAdminRequest(method, path, body, nonce string) *http.Request {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	req.Header.Set("X-Timestamp", timestamp)
	req.Header.Set("X-Nonce", nonce)
	query, _ := url.ParseQuery(req.URL.RawQuery)
	req.Header.Set("X-Signature", SignAdmin(testAdminSecret, method, req.URL.EscapedPath(), query.Encode(), timestamp, nonce, []byte(body)))
	return req
}

func TestAdminMiddleware(t *testing.T) {
	var received string
	handler := AdminMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		received = string(raw)
		w.WriteHeader(http.StatusNoContent)
	}), "/api/core/admin", testAdminSecret)

	call := func(req *http.Request) int {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Code
	}
	req := signedAdminRequest(http.MethodPut, "/api/core/admin/services/1?env=test&b=2", `{"enabled":true}`, "0123456789abcdef")
	if status := call(req); status != http.StatusNoContent || received != `{"enabled":true}` {
		t.Fatalf("valid signature: status=%d body=%q", status, received)
	}
	if status := call(signedAdminRequest(http.MethodGet, "/api/core/admin/services", "", "1111111111111111")); status != http.StatusNoContent {
		t.Fatalf("valid GET signature: %d", status)
	}
	if status := call(httptest.NewRequest(http.MethodGet, "/api/core/admin/services", nil)); status != http.StatusUnauthorized {
		t.Fatalf("unsigned GET: %d", status)
	}
	if status := call(signedAdminRequest(http.MethodPut, "/api/core/admin/services/1?env=test&b=2", `{"enabled":true}`, "0123456789abcdef")); status != http.StatusConflict {
		t.Fatalf("nonce replay: %d", status)
	}
	tampered := signedAdminRequest(http.MethodPut, "/api/core/admin/services/1", `{"enabled":true}`, "2222222222222222")
	tampered.Body = io.NopCloser(strings.NewReader(`{"enabled":false}`))
	if status := call(tampered); status != http.StatusUnauthorized {
		t.Fatalf("tampered body: %d", status)
	}
	wrongPath := signedAdminRequest(http.MethodGet, "/api/core/admin/services", "", "3333333333333333")
	wrongPath.URL.Path = "/api/core/admin/channel-apps"
	if status := call(wrongPath); status != http.StatusUnauthorized {
		t.Fatalf("tampered path: %d", status)
	}
	other := httptest.NewRequest(http.MethodGet, "/api/core/config/prod/app", nil)
	if status := call(other); status != http.StatusNoContent {
		t.Fatalf("business API unexpectedly blocked: %d", status)
	}
	lookalike := httptest.NewRequest(http.MethodGet, "/api/core/administrator", nil)
	if status := call(lookalike); status != http.StatusNoContent {
		t.Fatalf("prefix lookalike unexpectedly blocked: %d", status)
	}
}

func TestAdminSignatureRejectsExpiredAndMissingKey(t *testing.T) {
	okHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	guard := AdminMiddleware(okHandler, "/api/config/admin", testAdminSecret)
	expired := signedAdminRequest(http.MethodGet, "/api/config/admin/nodes", "", "4444444444444444")
	old := strconv.FormatInt(time.Now().Add(-10*time.Minute).Unix(), 10)
	expired.Header.Set("X-Timestamp", old)
	expired.Header.Set("X-Signature", SignAdmin(testAdminSecret, expired.Method, expired.URL.EscapedPath(), "", old, expired.Header.Get("X-Nonce"), nil))
	rec := httptest.NewRecorder()
	guard.ServeHTTP(rec, expired)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expired signature: %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	AdminMiddleware(okHandler, "/api/config/admin", "").ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/config/admin/nodes", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("unconfigured secret: %d", rec.Code)
	}
}

func TestAdminSignatureBindsQueryAndMethod(t *testing.T) {
	gate := AdminMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}), "/api/core/admin", testAdminSecret)
	req := signedAdminRequest(http.MethodGet, "/api/core/admin/services?env=prod", "", "5555555555555555")
	req.URL.RawQuery = "env=test"
	rec := httptest.NewRecorder()
	gate.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("tampered query: %d", rec.Code)
	}

	req = signedAdminRequest(http.MethodGet, "/api/core/admin/services", "", "6666666666666666")
	req.Method = http.MethodDelete
	rec = httptest.NewRecorder()
	gate.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("tampered method: %d", rec.Code)
	}
}

func TestAdminMiddlewareAlwaysRequiresSignature(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	guard := AdminMiddleware(next, "/api/config/admin", testAdminSecret)
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete} {
		recorder := httptest.NewRecorder()
		guard.ServeHTTP(recorder, httptest.NewRequest(method, "/api/config/admin/nodes", nil))
		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("unsigned %s: HTTP %d", method, recorder.Code)
		}
	}
}

// 固定测试向量锁定管理接口 HMAC-SHA256 协议。
func TestSignAdminHMACSHA256KnownVector(t *testing.T) {
	got := SignAdmin(testAdminSecret, http.MethodGet, "/api/core/admin/services", "", "1793865600", "0123456789abcdef", nil)
	const want = "bddff8f8d31b4b4a220740c6f112502badad53a3c912bb745fd70e9e1db9ec1d"
	if got != want {
		t.Fatalf("管理接口 HMAC-SHA256 签名不匹配: got=%s want=%s", got, want)
	}
}

func TestRequestHMACMiddlewareProtectsMultiplePrefixes(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	guard := RequestHMACMiddleware(next, []string{"/api/config/sync", "/api/config/nodes"}, testAdminSecret)
	for i, item := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/config/sync/revision"},
		{http.MethodGet, "/api/config/sync/snapshot"},
		{http.MethodGet, "/api/config/nodes/12"},
		{http.MethodPost, "/api/config/nodes/batch-get"},
	} {
		nonce := strconv.FormatInt(int64(i+1), 16)
		nonce = strings.Repeat("0", 16-len(nonce)) + nonce
		req := signedAdminRequest(item.method, item.path, "", nonce)
		rec := httptest.NewRecorder()
		guard.ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("signed request %s rejected: %d", item.path, rec.Code)
		}
	}
	for _, path := range []string{"/api/config/sync-other", "/api/config/nodes-other"} {
		rec := httptest.NewRecorder()
		guard.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusNoContent {
			t.Fatalf("unmatched path %s was protected: %d", path, rec.Code)
		}
	}
}
