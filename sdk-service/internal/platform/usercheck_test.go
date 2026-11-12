package platform

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestUserCheckEndpointAndTimeout(t *testing.T) {
	p, err := NewPublisher("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.CheckUser(context.Background(), "app", "secret", []byte(`{}`)); !errors.Is(err, ErrPublishEndpointMissing) {
		t.Fatalf("missing endpoint: %v", err)
	}
	if _, err := NewPublisher("", "", "https://example.com/other"); err == nil {
		t.Fatal("accepted incorrect path")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { time.Sleep(100 * time.Millisecond) }))
	defer srv.Close()
	p, err = NewPublisher("", "", srv.URL+"/cp/user/check")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := p.CheckUser(ctx, "app", "secret", []byte(`{}`)); !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, ErrPublishRejected) {
		t.Fatalf("timeout: %v", err)
	}
}
