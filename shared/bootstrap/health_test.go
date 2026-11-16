package bootstrap

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthStateReadinessAndLiveness(t *testing.T) {
	health := NewHealthState()
	handler := health.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	request := func(path string) *httptest.ResponseRecorder {
		t.Helper()
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		return recorder
	}

	if got := request("/health/ready"); got.Code != http.StatusServiceUnavailable {
		t.Fatalf("initial readiness = %d, want %d", got.Code, http.StatusServiceUnavailable)
	}

	health.SetReady(true)
	if got := request("/health/ready"); got.Code != http.StatusNoContent {
		t.Fatalf("ready status = %d, want %d", got.Code, http.StatusNoContent)
	}

	health.SetReady(false)
	unready := request("/health/ready")
	if unready.Code != http.StatusServiceUnavailable {
		t.Fatalf("draining readiness = %d, want %d", unready.Code, http.StatusServiceUnavailable)
	}
	if got := unready.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("readiness Cache-Control = %q, want no-store", got)
	}

	live := request("/health/live")
	if live.Code != http.StatusNoContent {
		t.Fatalf("liveness during draining = %d, want %d", live.Code, http.StatusNoContent)
	}
	if got := live.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("liveness Cache-Control = %q, want no-store", got)
	}
}
