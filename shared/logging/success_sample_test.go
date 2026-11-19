package logging

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestPublicServiceSuccessSamplingIsPerRouteAndConcurrent(t *testing.T) {
	var logs bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	defer slog.SetDefault(old)

	cfg := Default()
	cfg.SuccessSampleEvery = 100
	cfg.SlowThreshold = "1h"
	handler := HTTP(cfg, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	var wg sync.WaitGroup
	for _, path := range []string{
		"/api/service/user/check/prod",
		"/api/service/role/query/prod",
		"/api/service/server/query/prod",
		"/api/service/role/control/prod",
		"/api/service/subscribe/prod",
	} {
		for i := 0; i < 100; i++ {
			wg.Add(1)
			go func(path string) {
				defer wg.Done()
				handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, path, nil))
			}(path)
		}
	}
	wg.Wait()
	if got := strings.Count(logs.String(), "HTTP 请求"); got != 5 {
		t.Fatalf("expected one sampled success per service route, got %d: %s", got, logs.String())
	}
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/core/pay/notify/app", nil))
	if got := strings.Count(logs.String(), "HTTP 请求"); got != 6 {
		t.Fatalf("critical payment route should not be sampled, got %d", got)
	}
}

func TestSuccessSamplingPreservesErrorsAndSlowRequests(t *testing.T) {
	var logs bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	defer slog.SetDefault(old)
	cfg := Default()
	cfg.SuccessSampleEvery = 1000
	cfg.SlowThreshold = "1ms"
	handler := HTTP(cfg, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("fail") == "1" {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		time.Sleep(2 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodPost, "/api/service/user/check/prod", nil)
	handler.ServeHTTP(httptest.NewRecorder(), req)
	req = httptest.NewRequest(http.MethodPost, "/api/service/user/check/prod", nil)
	req.Header.Set("fail", "1")
	handler.ServeHTTP(httptest.NewRecorder(), req)
	if got := strings.Count(logs.String(), "HTTP 请求"); got != 2 {
		t.Fatalf("slow and failed requests must bypass sampling, got %d: %s", got, logs.String())
	}
}

func TestAccessDisabledStillRecordsFailureAndSlow(t *testing.T) {
	var logs bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	defer slog.SetDefault(old)
	cfg := Default()
	cfg.AccessEnabled = false
	cfg.SlowThreshold = "1h"
	handler := HTTP(cfg, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("fail") == "1" {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/normal", nil))
	req := httptest.NewRequest(http.MethodGet, "/broken", nil)
	req.Header.Set("fail", "1")
	handler.ServeHTTP(httptest.NewRecorder(), req)
	if got := strings.Count(logs.String(), "HTTP 请求"); got != 1 {
		t.Fatalf("expected only failed request with access disabled, got %d", got)
	}
}

func TestSuccessSampleEveryEnvValidation(t *testing.T) {
	cfg := Default()
	t.Setenv("LOG_SUCCESS_SAMPLE_EVERY", "1000")
	if err := cfg.ApplyEnv(); err != nil || cfg.SuccessSampleEvery != 1000 {
		t.Fatalf("invalid env config: %+v %v", cfg, err)
	}
	t.Setenv("LOG_SUCCESS_SAMPLE_EVERY", "-1")
	if err := cfg.ApplyEnv(); err == nil {
		t.Fatal("negative success sample interval accepted")
	}
}
