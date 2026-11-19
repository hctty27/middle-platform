package platform

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestPublisherSamplesSuccessPerServiceButKeepsFailures(t *testing.T) {
	status := http.StatusOK
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"code":0,"data":true}`))
	}))
	defer server.Close()
	p, err := NewPublisherWithOptions(server.URL+"/report/event",
		server.URL+"/game/subscribe/send-message", server.URL+"/cp/user/check", PublisherOptions{
			MaxIdlePerHost: 32, SlowThreshold: time.Hour,
			PlatformSuccessSampleEvery: 10,
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
	if got := p.userCheckSuccesses.Load(); got != 10 {
		t.Fatalf("user check sampler count=%d", got)
	}
	if got := p.subscribeSuccesses.Load(); got != 10 {
		t.Fatalf("subscribe sampler count=%d", got)
	}
	if got := p.eventSuccesses.Load(); got != 10 {
		t.Fatalf("event sampler count=%d", got)
	}

	status = http.StatusBadGateway
	if err := p.Send(context.Background(), "subscribe_message", "app", "secret", []byte(`{}`)); err == nil {
		t.Fatal("expected platform failure")
	}
	if got := p.subscribeSuccesses.Load(); got != 10 {
		t.Fatalf("failed request must not increment success sampler, count=%d", got)
	}
}
