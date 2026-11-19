package logging

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPAccessEnabledControlsAllSuccessfulRequests(t *testing.T) {
	for _, tc := range []struct {
		name    string
		enabled bool
		want    int
	}{
		{"enabled", true, 300},
		{"disabled", false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var logs bytes.Buffer
			previous := slog.Default()
			slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
			defer slog.SetDefault(previous)
			cfg := Default()
			cfg.AccessEnabled = tc.enabled
			cfg.SlowThreshold = "1h"
			handler := HTTP(cfg, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))
			for i := 0; i < 100; i++ {
				for _, route := range []struct{ method, path string }{
					{http.MethodGet, "/api/core/config/prod/app"},
					{http.MethodPost, "/api/service/events/prod"},
					{http.MethodPost, "/api/service/user/check/prod"},
				} {
					recorder := httptest.NewRecorder()
					handler.ServeHTTP(recorder, httptest.NewRequest(route.method, route.path, nil))
					if recorder.Header().Get(RequestIDHeader) == "" {
						t.Fatal("request ID must be present even when access logging is off")
					}
				}
			}
			if got := strings.Count(logs.String(), "HTTP 请求"); got != tc.want {
				t.Fatalf("accessEnabled=%t: expected %d access records, got %d", tc.enabled, tc.want, got)
			}
		})
	}
}

func TestAccessDisabledStillRecordsWarningsAndErrors(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	defer slog.SetDefault(previous)
	cfg := Default()
	cfg.AccessEnabled = false
	cfg.SlowThreshold = "1h"
	handler := HTTP(cfg, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/bad":
			w.WriteHeader(http.StatusBadRequest)
		case "/failure":
			w.WriteHeader(http.StatusServiceUnavailable)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	for _, path := range []string{"/ok", "/bad", "/failure"} {
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, path, nil))
	}
	if got := strings.Count(logs.String(), "HTTP 请求"); got != 2 {
		t.Fatalf("expected WARN and ERROR only, got %d: %s", got, logs.String())
	}
	if !strings.Contains(logs.String(), "\"level\":\"WARN\"") ||
		!strings.Contains(logs.String(), "\"level\":\"ERROR\"") {
		t.Fatalf("missing warning or error: %s", logs.String())
	}
}

func TestPlatformSamplerEnvAndLegacySettings(t *testing.T) {
	cfg := Default()
	if cfg.PlatformSuccessSampleEvery != 1000 {
		t.Fatalf("unexpected default platform sampler: %d", cfg.PlatformSuccessSampleEvery)
	}
	t.Setenv("LOG_PLATFORM_SUCCESS_SAMPLE_EVERY", "25")
	if err := cfg.ApplyEnv(); err != nil || cfg.PlatformSuccessSampleEvery != 25 {
		t.Fatalf("new platform sampler env not applied: %+v %v", cfg, err)
	}
	t.Setenv("LOG_PLATFORM_SUCCESS_SAMPLE_EVERY", "-1")
	if err := cfg.ApplyEnv(); err == nil {
		t.Fatal("invalid platform sampling interval accepted")
	}
	t.Setenv("LOG_PLATFORM_SUCCESS_SAMPLE_EVERY", "25")
	t.Setenv("LOG_SDK_EVENT_SAMPLE_EVERY", "10")
	if err := cfg.ApplyEnv(); err == nil || !strings.Contains(err.Error(), "已废弃") {
		t.Fatalf("legacy sampling variable must report migration error: %v", err)
	}
}
