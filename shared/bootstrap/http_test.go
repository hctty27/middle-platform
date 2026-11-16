package bootstrap

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestRequestTimeoutDoesNotRejectConcurrentRequests(t *testing.T) {
	const count = 64
	entered, release := make(chan struct{}, count), make(chan struct{})
	h := WithRequestTimeout(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := r.Context().Deadline(); !ok {
			t.Error("missing request deadline")
		}
		entered <- struct{}{}
		<-release
		w.WriteHeader(204)
	}), 5*time.Second)
	var wg sync.WaitGroup
	defer func() { close(release); wg.Wait() }()
	for range count {
		wg.Go(func() {
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, httptest.NewRequest("POST", "/", nil))
			if rr.Code != 204 || rr.Header().Get("Retry-After") != "" {
				t.Errorf("request rejected: %d", rr.Code)
			}
		})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for range count {
		select {
		case <-entered:
		case <-ctx.Done():
			t.Fatal("request concurrency was limited")
		}
	}
}

func TestRequestTimeoutWaitsForActualReturn(t *testing.T) {
	entered, cancelled, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	h := WithRequestTimeout(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-r.Context().Done()
		close(cancelled)
		<-release
		w.WriteHeader(204)
	}), time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer func() { close(release); <-done }()
	go func() {
		defer close(done)
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/", nil).WithContext(ctx))
	}()
	<-entered
	cancel()
	<-cancelled
	select {
	case <-done:
		t.Fatal("wrapper returned while handler was running")
	default:
	}
}
