package logging

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEventSamplingKeepsUserChecksAndFailures(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	defer slog.SetDefault(previous)
	cfg := Default()
	cfg.SDKEventSampleEvery = 10
	h := HTTP(cfg, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("fail") == "1" {
			w.WriteHeader(502)
			return
		}
		w.WriteHeader(200)
	}))
	for i := 0; i < 100; i++ {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/api/service/events/test", nil))
	}
	for i := 0; i < 5; i++ {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/api/service/user/check/test", nil))
	}
	req := httptest.NewRequest("POST", "/api/service/events/test", nil)
	req.Header.Set("fail", "1")
	h.ServeHTTP(httptest.NewRecorder(), req)
	if count := strings.Count(logs.String(), "HTTP 请求"); count != 16 {
		t.Fatalf("expected 10 sampled successes, five user checks and failure, got %d", count)
	}
}
