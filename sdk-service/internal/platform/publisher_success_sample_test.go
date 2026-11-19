package platform

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPublisherSamplesSuccessPerServiceButKeepsFailures(t *testing.T) {
	var logs bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	defer slog.SetDefault(old)

	status := http.StatusOK
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"code":0,"data":true}`))
	}))
	defer server.Close()
	p, err := NewPublisherWithOptions(server.URL+"/report/event",
		server.URL+"/game/subscribe/send-message", server.URL+"/cp/user/check", PublisherOptions{
			MaxIdlePerHost: 32, SlowThreshold: time.Hour,
			EventSampleEvery: 10, SuccessSampleEvery: 10,
		})
	if err != nil {
		t.Fatal(err)
	}
	defer p.CloseIdleConnections()

	for i := 0; i < 10; i++ {
		if ok, err := p.CheckUser(context.Background(), "app", "secret", []byte(`{}`)); err != nil || !ok {
			t.Fatalf("user check: ok=%t err=%v", ok, err)
		}
		if err := p.Send(context.Background(), "subscribe_message", "app", "secret", []byte(`{}`)); err != nil {
			t.Fatal(err)
		}
		if err := p.Send(context.Background(), "event_report", "app", "secret", []byte(`{}`)); err != nil {
			t.Fatal(err)
		}
	}
	if got := strings.Count(logs.String(), "平台接口请求完成"); got != 3 {
		t.Fatalf("expected one success log per platform service, got %d: %s", got, logs.String())
	}
	status = http.StatusBadGateway
	if err := p.Send(context.Background(), "subscribe_message", "app", "secret", []byte(`{}`)); err == nil {
		t.Fatal("expected platform failure")
	}
	if !strings.Contains(logs.String(), "平台接口请求失败") || !strings.Contains(logs.String(), "\"level\":\"WARN\"") {
		t.Fatalf("failed outbound call must not be sampled out: %s", logs.String())
	}
}
