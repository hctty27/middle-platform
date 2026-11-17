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

func TestNonceCacheCapacityAndExpiry(t *testing.T) {
	now := time.Unix(1800000000, 0)
	c := &adminNonceCache{limit: 2}
	if !c.use("a", now.Unix()+300, now) || !c.use("b", now.Unix()+600, now) {
		t.Fatal("initial nonces rejected")
	}
	if c.use("a", now.Unix()+300, now) || c.use("c", now.Unix()+300, now) {
		t.Fatal("replay or overflow accepted")
	}
	later := now.Add(300 * time.Second)
	if !c.use("c", later.Unix()+300, later) {
		t.Fatal("expired slot not reclaimed")
	}
	if c.use("b", later.Unix()+300, later) {
		t.Fatal("future timestamp nonce evicted early")
	}
	if len(c.used) != 2 {
		t.Fatalf("unexpected size: %d", len(c.used))
	}
}

func TestNonceCacheConcurrentReplay(t *testing.T) {
	now := time.Unix(1800000000, 0)
	c := &adminNonceCache{limit: 10}
	var accepted atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if c.use("same", now.Unix()+300, now) {
				accepted.Add(1)
			}
		}()
	}
	wg.Wait()
	if accepted.Load() != 1 {
		t.Fatalf("accepted %d concurrent replays", accepted.Load())
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
