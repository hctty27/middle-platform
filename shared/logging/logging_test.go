package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func capture(t *testing.T) *bytes.Buffer {
	t.Helper()
	buffer := new(bytes.Buffer)
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(buffer, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return buffer
}

func TestConfigAndEnvironment(t *testing.T) {
	cfg := Default()
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("LOG_ACCESS_ENABLED", "false")
	if err := cfg.ApplyEnv(); err != nil { t.Fatal(err) }
	if cfg.Level != "debug" || cfg.AccessEnabled { t.Fatalf("%+v", cfg) }
	cfg.Level = "trace"
	if cfg.Validate() == nil { t.Fatal("invalid level accepted") }
	cfg = Default()
	cfg.SlowThreshold = "0s"
	if cfg.Validate() == nil { t.Fatal("zero threshold accepted") }
}

func TestHTTPLogsWithoutQueryOrBody(t *testing.T) {
	out := capture(t)
	cfg := Default()
	h := HTTP(cfg, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if RequestID(r.Context()) != "from-proxy" { t.Fatal("request id not propagated") }
		if FromContext(r.Context()) == nil { t.Fatal("missing request logger") }
		w.WriteHeader(http.StatusCreated)
	}))
	request := httptest.NewRequest(http.MethodPost, "/api/config/nodes?token=private", strings.NewReader("super-secret"))
	request.Header.Set(RequestIDHeader, "from-proxy")
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, request)
	if recorder.Code != 201 || recorder.Header().Get(RequestIDHeader) != "from-proxy" {
		t.Fatalf("unexpected response: %d %v", recorder.Code, recorder.Header())
	}
	line := out.String()
	if strings.Contains(line, "private") || strings.Contains(line, "super-secret") {
		t.Fatalf("sensitive request data leaked: %s", line)
	}
	var event map[string]any
	if err := json.Unmarshal(out.Bytes(), &event); err != nil { t.Fatal(err) }
	if event["request_id"] != "from-proxy" || event["status"] != float64(201) {
		t.Fatalf("unexpected access log: %v", event)
	}
}

func TestHTTPInvalidIDAndPanic(t *testing.T) {
	out := capture(t)
	handler := HTTP(Default(), http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("do-not-leak")
	}))
	request := httptest.NewRequest(http.MethodGet, "/panic", nil)
	request.Header.Set(RequestIDHeader, "bad\nheader")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != 500 || !validRequestID(recorder.Header().Get(RequestIDHeader)) {
		t.Fatalf("unexpected response: %d %v", recorder.Code, recorder.Header())
	}
	if strings.Contains(out.String(), "do-not-leak") { t.Fatal("panic value leaked") }
	if !strings.Contains(out.String(), "http handler panic") { t.Fatal("panic not logged") }
}

func TestHTTPHealthAndAccessDisabled(t *testing.T) {
	out := capture(t)
	cfg := Default()
	cfg.AccessEnabled = false
	handler := HTTP(cfg, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/bad" { w.WriteHeader(503) }
	}))
	for _, path := range []string{"/health/live", "/ok", "/bad"} {
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", path, nil))
	}
	if strings.Count(out.String(), "http request") != 1 || !strings.Contains(out.String(), "503") {
		t.Fatalf("expected only failed request log: %s", out.String())
	}
}

func TestHTTPLogsSlowRequests(t *testing.T) {
	out := capture(t)
	cfg := Default()
	cfg.AccessEnabled = false
	cfg.SlowThreshold = "1ms"
	h := HTTP(cfg, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(3 * time.Millisecond)
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/slow", nil))
	if !strings.Contains(out.String(), "http request") || !strings.Contains(out.String(), "\"level\":\"WARN\"") {
		t.Fatalf("slow request should warn: %s", out.String())
	}
}

func TestRequestIDMissing(t *testing.T) {
	if RequestID(context.Background()) != "" { t.Fatal("unexpected request id") }
}
