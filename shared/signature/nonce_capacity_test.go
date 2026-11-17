package signature

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestNonceGuardCapacityAndExpiry(t *testing.T) {
	now := time.Unix(1800000000, 0)
	guard := NewNonceGuard(2)
	use := func(nonce string, timestamp time.Time, at time.Time) bool {
		accepted, err := guard.Use(context.Background(), "admin", nonce,
			strconv.FormatInt(timestamp.Unix(), 10), at, DefaultReplayWindow)
		if err != nil {
			t.Fatal(err)
		}
		return accepted
	}
	if !use("a", now, now) || !use("b", now.Add(5*time.Minute), now) {
		t.Fatal("initial nonces rejected")
	}
	if use("a", now, now) || use("c", now, now) {
		t.Fatal("replay or overflow accepted")
	}
	later := now.Add(5*time.Minute + time.Second)
	if !use("c", later, later) {
		t.Fatal("expired slot not reclaimed")
	}
	if use("b", later, later) {
		t.Fatal("future timestamp nonce evicted early")
	}
	if len(guard.used) != 2 {
		t.Fatalf("unexpected size: %d", len(guard.used))
	}
}

func TestNonceGuardConcurrentReplay(t *testing.T) {
	now := time.Unix(1800000000, 0)
	guard := NewNonceGuard(10)
	var accepted atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := guard.Use(context.Background(), "admin", "same",
				strconv.FormatInt(now.Unix(), 10), now, DefaultReplayWindow)
			if err != nil {
				t.Errorf("Use returned error: %v", err)
				return
			}
			if ok {
				accepted.Add(1)
			}
		}()
	}
	wg.Wait()
	if accepted.Load() != 1 {
		t.Fatalf("accepted %d concurrent replays", accepted.Load())
	}
}

func TestNonceGuardScopesAndTimestampWindow(t *testing.T) {
	now := time.Unix(1800000000, 0)
	guard := NewNonceGuard(10)
	timestamp := strconv.FormatInt(now.Unix(), 10)
	for _, scope := range []string{"admin", "subscriber:access-a"} {
		ok, err := guard.Use(context.Background(), scope, "same", timestamp, now, DefaultReplayWindow)
		if err != nil || !ok {
			t.Fatalf("scope %q should accept an independent nonce: ok=%v err=%v", scope, ok, err)
		}
	}
	stale := strconv.FormatInt(now.Add(-6*time.Minute).Unix(), 10)
	if ok, err := guard.Use(context.Background(), "admin", "stale", stale, now, DefaultReplayWindow); err == nil || ok {
		t.Fatalf("stale timestamp accepted: ok=%v err=%v", ok, err)
	}
}

func TestSubscriberNonceCapacityAboveLegacyLimit(t *testing.T) {
	const secret = "subscriber-secret-0123456789abcdef"
	const path = "/api/config/sync/revision"
	guard := RequestSubscriberHMACMiddlewareWithNonceLimit(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }),
		[]string{"/api/config/sync"},
		func(context.Context, string) (string, bool, error) { return secret, true, nil },
		10002,
	)
	call := func(code string, i int) int {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		ts := strconv.FormatInt(time.Now().Unix(), 10)
		nonce := fmt.Sprintf("%016x", i)
		req.Header.Set("X-Subscriber-Code", code)
		req.Header.Set("X-Timestamp", ts)
		req.Header.Set("X-Nonce", nonce)
		req.Header.Set("X-Signature", SignSubscriberHMACRequest(secret, code, req.Method, path, "", ts, nonce, nil))
		rec := httptest.NewRecorder()
		guard.ServeHTTP(rec, req)
		return rec.Code
	}
	for i := 0; i < 10001; i++ {
		if got := call("a", i); got != http.StatusNoContent {
			t.Fatalf("nonce %d rejected: %d", i, got)
		}
	}
	if got := call("b", 0); got != http.StatusNoContent {
		t.Fatalf("independent subscriber rejected: %d", got)
	}
	if got := call("a", 0); got != http.StatusConflict {
		t.Fatalf("replay status: %d", got)
	}
	if got := call("a", 10001); got != http.StatusConflict {
		t.Fatalf("overflow status: %d", got)
	}
}
