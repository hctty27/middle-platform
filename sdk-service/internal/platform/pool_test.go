package platform

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestUserCheckHasIndependentTransport(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/report/event" {
			close(started)
			<-release
		}
		_, _ = w.Write([]byte(`{"code":0,"data":true}`))
	}))
	defer srv.Close()
	defer close(release)
	p, err := NewPublisherWithOptions(srv.URL+"/report/event", "", srv.URL+"/cp/user/check", PublisherOptions{
		IsolateConnections: true, MaxIdlePerHost: 4, EventMaxConns: 1,
		PlatformSuccessSampleEvery: 1, SlowThreshold: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer p.CloseIdleConnections()
	if p.client.Transport == p.eventClient.Transport {
		t.Fatal("transport shared with event workload")
	}
	done := make(chan error, 1)
	go func() { done <- p.Send(context.Background(), "event_report", "app", "secret", []byte(`{}`)) }()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("event did not reach platform")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if ok, err := p.CheckUser(ctx, "app", "secret", []byte(`{}`)); err != nil || !ok {
		t.Fatalf("user check blocked by event: %v", err)
	}
	select {
	case err := <-done:
		t.Fatalf("event unexpectedly completed: %v", err)
	default:
	}
}

func TestPublisherRejectsInvalidPoolSettings(t *testing.T) {
	for _, options := range []PublisherOptions{
		{MaxIdlePerHost: 0, SlowThreshold: time.Second},
		{MaxIdlePerHost: 32, EventMaxConns: 1, SlowThreshold: time.Second},
		{MaxIdlePerHost: 32, PlatformSuccessSampleEvery: -1, SlowThreshold: time.Second},
	} {
		if _, err := NewPublisherWithOptions("", "", "", options); err == nil {
			t.Fatal("invalid options accepted")
		}
	}
}
