package platform

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hitall.com/middle-platform/shared/logging"
)

func TestPublisherLogsRequestBodyAtDebug(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(previous)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":123,"data":false,"message":"private-response"}`))
	}))
	defer server.Close()
	publisher, err := NewPublisher("", "", server.URL+"/cp/user/check")
	if err != nil {
		t.Fatal(err)
	}
	handler := logging.HTTP(logging.Default(), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := publisher.CheckUser(r.Context(), "app-123", "private-secret", []byte(`{"open_id":"private-user","token":"private-token"}`))
		if err == nil {
			t.Error("expected platform rejection")
		}
	}))
	req := httptest.NewRequest(http.MethodPost, "/test", nil)
	req.Header.Set(logging.RequestIDHeader, "trace-123")
	handler.ServeHTTP(httptest.NewRecorder(), req)
	output := logs.String()
	for _, expected := range []string{`"request_id":"trace-123"`, `"service_code":"user_check"`, `"path":"/cp/user/check"`, `"http_status":200`, `"business_code":123`, `"failure":"business_rejected"`, `private-user`, `private-token`, `"request_body":`, `"response_body":`, `private-response`, `"response_truncated":false`} {
		if !strings.Contains(output, expected) {
			t.Errorf("log missing %s: %s", expected, output)
		}
	}
	combined := 0
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, `"request_body":`) && strings.Contains(line, `"response_body":`) {
			combined++
		}
	}
	if combined != 1 {
		t.Fatalf("expected one combined request and response log: %s", output)
	}
	for _, sensitive := range []string{"private-secret", "x-signature"} {
		if strings.Contains(output, sensitive) {
			t.Errorf("log contains sensitive value %q: %s", sensitive, output)
		}
	}
}

func TestPublisherDoesNotLogRequestBodyAtInfo(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	defer slog.SetDefault(previous)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":0,"data":true}`))
	}))
	defer server.Close()
	publisher, err := NewPublisher(server.URL+"/report/event", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := publisher.Send(context.Background(), "event_report", "app", "secret", []byte(`{"event":"private-value"}`)); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(logs.String(), "private-value") || strings.Contains(logs.String(), "request_body") || strings.Contains(logs.String(), "response_body") {
		t.Fatalf("request body logged at info level: %s", logs.String())
	}
}

func TestPublisherLogsPlatformResponses(t *testing.T) {
	for _, tc := range []struct {
		name      string
		status    int
		body      string
		truncated bool
	}{
		{"success", 200, `{"code":0,"data":true}`, false},
		{"http_error", 502, `platform error response`, false},
		{"oversized", 200, strings.Repeat("x", 8192) + "hidden-suffix", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var logs bytes.Buffer
			previous := slog.Default()
			slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
			defer slog.SetDefault(previous)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			publisher, err := NewPublisher(server.URL+"/report/event", "")
			if err != nil {
				t.Fatal(err)
			}
			err = publisher.Send(context.Background(), "event_report", "app", "secret", []byte(`{}`))
			if (err == nil) != (tc.name == "success") {
				t.Fatalf("unexpected result: %v", err)
			}
			output := logs.String()
			if !strings.Contains(output, `"response_body":`) || !strings.Contains(output, `"response_read_error":false`) {
				t.Fatalf("missing response log: %s", output)
			}
			if tc.truncated {
				if !strings.Contains(output, `"response_truncated":true`) || strings.Contains(output, "hidden-suffix") {
					t.Fatal("oversized response was not truncated")
				}
			} else if !strings.Contains(output, `"response_truncated":false`) || (tc.name == "http_error" && !strings.Contains(output, tc.body)) {
				t.Fatalf("incorrect response log: %s", output)
			}
		})
	}
}

func TestPublisherLogsTransportFailure(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(previous)

	publisher, err := NewPublisher("http://example.com/report/event", "")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := publisher.Send(ctx, "event_report", "app", "secret", []byte(`{}`)); err == nil {
		t.Fatal("expected transport failure")
	}
	if !strings.Contains(logs.String(), `"failure":"transport"`) {
		t.Fatalf("missing transport failure: %s", logs.String())
	}
	if strings.Contains(logs.String(), `"request_body":`) || strings.Contains(logs.String(), `"response_body":`) {
		t.Fatalf("unexpected request and response log without a response: %s", logs.String())
	}
}
