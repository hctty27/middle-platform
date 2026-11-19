package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestBestEffortEventHTTPStatusLogs(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		level  string
		msg    string
	}{
		{"ok", http.StatusOK, "INFO", "平台接口请求完成"},
		{"accepted", http.StatusAccepted, "INFO", "平台接口请求完成"},
		{"no_content", http.StatusNoContent, "INFO", "平台接口请求完成"},
		{"redirect", http.StatusFound, "WARN", "平台接口请求失败"},
		{"rate_limited", http.StatusTooManyRequests, "WARN", "平台接口请求失败"},
		{"server_error", http.StatusInternalServerError, "WARN", "平台接口请求失败"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var logs bytes.Buffer
			oldLogger := slog.Default()
			slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
			defer slog.SetDefault(oldLogger)

			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if r.Method != http.MethodPost || r.URL.Path != "/report/event" {
					t.Errorf("unexpected outbound request: %s %s", r.Method, r.URL.Path)
				}
				w.WriteHeader(tc.status)
				_, _ = fmt.Fprint(w, "not-json-private-platform-response")
			}))
			defer server.Close()
			publisher, err := NewPublisher(server.URL+"/report/event", "")
			if err != nil {
				t.Fatal(err)
			}
			publisher.SendEventBestEffort(context.Background(), "app-123", "platform-secret", []byte("private-event-payload"))
			if requests != 1 {
				t.Fatalf("best-effort mode must not retry: got %d requests", requests)
			}
			var record map[string]any
			if err := json.Unmarshal(logs.Bytes(), &record); err != nil {
				t.Fatalf("invalid log: %v, output=%s", err, logs.String())
			}
			if record["level"] != tc.level || record["msg"] != tc.msg ||
				record["http_status"] != float64(tc.status) || record["report_mode"] != "async" {
				t.Fatalf("unexpected event log: %v", record)
			}
			if tc.level == "WARN" && record["failure"] != "http_status_error" {
				t.Fatalf("missing HTTP failure category: %v", record)
			}
			if tc.level == "INFO" {
				if _, exists := record["failure"]; exists {
					t.Fatalf("success log should not report failure: %v", record)
				}
			}
			if strings.Contains(logs.String(), "private-event-payload") ||
				strings.Contains(logs.String(), "not-json-private-platform-response") ||
				strings.Contains(logs.String(), "platform-secret") {
				t.Fatalf("request, response, or secret was logged: %s", logs.String())
			}
		})
	}
}

func TestBestEffortEventSamplingKeepsHTTPFailures(t *testing.T) {
	var logs bytes.Buffer
	oldLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	defer slog.SetDefault(oldLogger)
	status := http.StatusAccepted
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
	}))
	defer server.Close()
	publisher, err := NewPublisher(server.URL+"/report/event", "")
	if err != nil {
		t.Fatal(err)
	}
	publisher.platformSuccessSampleEvery = 100
	publisher.slowThreshold = time.Hour
	for _, code := range []int{http.StatusOK, http.StatusAccepted, http.StatusNoContent} {
		status = code
		publisher.SendEventBestEffort(context.Background(), "app", "secret", []byte("{}"))
	}
	if logs.Len() != 0 {
		t.Fatalf("successful fast event should be sampled: %s", logs.String())
	}
	status = http.StatusServiceUnavailable
	publisher.SendEventBestEffort(context.Background(), "app", "secret", []byte("{}"))
	if !strings.Contains(logs.String(), "\"http_status\":503") ||
		!strings.Contains(logs.String(), "\"failure\":\"http_status_error\"") ||
		!strings.Contains(logs.String(), "\"level\":\"WARN\"") {
		t.Fatalf("HTTP failures must bypass success sampling: %s", logs.String())
	}
}
