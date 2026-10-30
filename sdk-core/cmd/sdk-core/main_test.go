package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestWithDrainReadiness(t *testing.T) {
	var ready atomic.Bool
	ready.Store(true)
	handler := withDrainReadiness(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}), &ready)

	request := func(path string) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		return w
	}

	if got := request("/health/ready").Code; got != http.StatusNoContent {
		t.Fatalf("ready status = %d, want %d", got, http.StatusNoContent)
	}
	ready.Store(false)
	unready := request("/health/ready")
	if unready.Code != http.StatusServiceUnavailable || !strings.Contains(unready.Body.String(), "\"DOWN\"") {
		t.Fatalf("draining readiness = %d, body = %q", unready.Code, unready.Body.String())
	}
	if got := request("/health/live").Code; got != http.StatusNoContent {
		t.Fatalf("liveness during draining = %d, want %d", got, http.StatusNoContent)
	}
}
