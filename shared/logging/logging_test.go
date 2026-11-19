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

func TestHandlerRecordTimeBeijing(t *testing.T) {
	stamp := time.Date(2026, 11, 16, 2, 5, 6, 123456789, time.UTC)
	const want = "2026-11-16T10:05:06.123+08:00"
	if got := formatRecordTime(stamp); got != want {
		t.Fatalf("Beijing timestamp = %q, want %q", got, want)
	}
	for _, format := range []string{"json", "text"} {
		t.Run(format, func(t *testing.T) {
			var out bytes.Buffer
			cfg := Default()
			cfg.Format = format
			handler := newHandler(&out, cfg)
			record := slog.NewRecord(stamp, slog.LevelInfo, "event", 0)
			record.AddAttrs(slog.Group("payload", slog.Time("time", stamp)), slog.String("time_text", "unchanged"))
			if err := handler.Handle(context.Background(), record); err != nil {
				t.Fatal(err)
			}
			if format == "json" {
				var event struct {
					Time     string `json:"time"`
					TimeText string `json:"time_text"`
					Payload  struct {
						Time string `json:"time"`
					} `json:"payload"`
				}
				if err := json.Unmarshal(out.Bytes(), &event); err != nil {
					t.Fatal(err)
				}
				if event.Time != want || event.Payload.Time != stamp.Format(time.RFC3339Nano) || event.TimeText != "unchanged" {
					t.Fatalf("unexpected log: %s", out.String())
				}
			} else if !strings.HasPrefix(out.String(), "time="+want+" ") ||
				!strings.Contains(out.String(), "payload.time="+stamp.Truncate(time.Millisecond).Format(time.RFC3339Nano)) {
				t.Fatalf("unexpected log: %s", out.String())
			}
		})
	}
}

func TestConfigAndEnvironment(t *testing.T) {
	cfg := Default()
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("LOG_ACCESS_ENABLED", "false")
	if err := cfg.ApplyEnv(); err != nil {
		t.Fatal(err)
	}
	if cfg.Level != "debug" || cfg.AccessEnabled {
		t.Fatalf("%+v", cfg)
	}
	cfg.Level = "trace"
	if cfg.Validate() == nil {
		t.Fatal("invalid level accepted")
	}
	cfg = Default()
	cfg.SlowThreshold = "0s"
	if cfg.Validate() == nil {
		t.Fatal("zero threshold accepted")
	}
}

func TestHTTPLogsWithoutQueryOrBody(t *testing.T) {
	out := capture(t)
	cfg := Default()
	h := HTTP(cfg, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if RequestID(r.Context()) != "from-proxy" {
			t.Fatal("request id not propagated")
		}
		if FromContext(r.Context()) == nil {
			t.Fatal("missing request logger")
		}
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
	if err := json.Unmarshal(out.Bytes(), &event); err != nil {
		t.Fatal(err)
	}
	if event["request_id"] != "from-proxy" || event["status"] != float64(201) {
		t.Fatalf("unexpected access log: %v", event)
	}
}

// TestHTTPLogsActualPathWithoutMethodOrQuery keeps method and path separate
// and records real escaped path parameters instead of ServeMux route patterns.
func TestHTTPLogsActualPathWithoutMethodOrQuery(t *testing.T) {
	cases := []struct {
		name, method, pattern, target, wantPath string
	}{
		{"payment", http.MethodPost, "POST /api/core/pay/notify/{appid}", "/api/core/pay/notify/12345?token=hidden", "/api/core/pay/notify/12345"},
		{"config", http.MethodGet, "GET /api/core/config/{env}/{appid}", "/api/core/config/prod/98765?signature=hidden", "/api/core/config/prod/98765"},
		{"escaped", http.MethodPost, "POST /api/core/pay/notify/{appid}", "/api/core/pay/notify/12%2F34?token=hidden", "/api/core/pay/notify/12%2F34"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := capture(t)
			mux := http.NewServeMux()
			mux.HandleFunc(tc.pattern, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			})
			handler := HTTP(Default(), mux)
			handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(tc.method, tc.target, nil))
			var event map[string]any
			if err := json.Unmarshal(out.Bytes(), &event); err != nil {
				t.Fatalf("invalid log JSON: %v: %s", err, out.String())
			}
			if event["method"] != tc.method || event["path"] != tc.wantPath {
				t.Fatalf("method/path mismatch: %v", event)
			}
			if _, exists := event["route"]; exists {
				t.Fatalf("unexpected route field: %v", event)
			}
			if strings.Contains(out.String(), "hidden") {
				t.Fatalf("query parameters leaked into log: %s", out.String())
			}
		})
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
	if strings.Contains(out.String(), "do-not-leak") {
		t.Fatal("panic value leaked")
	}
	if !strings.Contains(recorder.Body.String(), "服务器内部错误") {
		t.Fatal("panic response must be Chinese")
	}
	if !strings.Contains(out.String(), "HTTP 处理请求时发生 panic") {
		t.Fatal("panic not logged")
	}
}

func TestHTTPHealthAndAccessDisabled(t *testing.T) {
	out := capture(t)
	cfg := Default()
	cfg.AccessEnabled = false
	handler := HTTP(cfg, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/bad" {
			w.WriteHeader(503)
		}
	}))
	for _, path := range []string{"/health/live", "/ok", "/bad"} {
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", path, nil))
	}
	if strings.Count(out.String(), "HTTP 请求") != 1 || !strings.Contains(out.String(), "503") {
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
	if !strings.Contains(out.String(), "HTTP 请求") || !strings.Contains(out.String(), "\"level\":\"WARN\"") {
		t.Fatalf("slow request should warn: %s", out.String())
	}
}

func TestRequestIDMissing(t *testing.T) {
	if RequestID(context.Background()) != "" {
		t.Fatal("unexpected request id")
	}
}

