// Local-only mock of the publishing platform and game notification endpoint.
package main

import (
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

func main() {
	mux := http.NewServeMux()
	var eventDelay atomic.Int64
	var eventFailure atomic.Bool
	var eventReceived atomic.Uint64
	var eventAccepted atomic.Uint64
	var userReceived atomic.Uint64
	// Local fixture controls, never exposed through Nginx.
	mux.HandleFunc("POST /load/event-mode", func(w http.ResponseWriter, r *http.Request) {
		delay, err := strconv.Atoi(r.URL.Query().Get("delay_ms"))
		mode := r.URL.Query().Get("mode")
		if err != nil || delay < 0 || delay > 100 || (mode != "ok" && mode != "fail") {
			http.Error(w, "invalid fixture mode", http.StatusBadRequest)
			return
		}
		eventDelay.Store(int64(delay))
		eventFailure.Store(mode == "fail")
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /load/stats", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]uint64{
			"event_received": eventReceived.Load(),
			"event_accepted": eventAccepted.Load(),
			"user_received":  userReceived.Load(),
		})
	})
	mux.HandleFunc("POST /report/event", func(w http.ResponseWriter, r *http.Request) {
		values := []string{"runner-load-platform-only", "load-app", r.Header.Get("x-timestamp"), r.Header.Get("x-nonce")}
		sort.Strings(values)
		sum := sha1.Sum([]byte(strings.Join(values, "_")))
		if r.Header.Get("x-app-id") != "load-app" || !hmac.Equal([]byte(r.Header.Get("x-signature")), []byte(hex.EncodeToString(sum[:]))) {
			http.Error(w, "bad platform signature", http.StatusUnauthorized)
			return
		}
		raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 65536))
		var payload struct {
			Event string         `json:"event"`
			Ext   map[string]any `json:"ext"`
		}
		if err != nil || json.Unmarshal(raw, &payload) != nil || payload.Event != "login" || payload.Ext == nil {
			http.Error(w, "bad fixture event", http.StatusBadRequest)
			return
		}
		eventReceived.Add(1)
		timer := time.NewTimer(time.Duration(eventDelay.Load()) * time.Millisecond)
		defer timer.Stop()
		select {
		case <-r.Context().Done():
			return
		case <-timer.C:
		}
		if eventFailure.Load() {
			http.Error(w, "injected event failure", http.StatusServiceUnavailable)
			return
		}
		eventAccepted.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, "{\"code\":0,\"data\":true}")
	})
	mux.HandleFunc("GET /health/ready", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "UP")
	})
	mux.HandleFunc("POST /cp/user/check", func(w http.ResponseWriter, r *http.Request) {
		values := []string{"runner-load-platform-only", "load-app", r.Header.Get("x-timestamp"), r.Header.Get("x-nonce")}
		sort.Strings(values)
		sum := sha1.Sum([]byte(strings.Join(values, "_")))
		if r.Header.Get("x-app-id") != "load-app" || !hmac.Equal([]byte(r.Header.Get("x-signature")), []byte(hex.EncodeToString(sum[:]))) {
			http.Error(w, "bad platform signature", http.StatusUnauthorized)
			return
		}
		_, _ = io.Copy(io.Discard, http.MaxBytesReader(w, r.Body, 65536))
		userReceived.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, "{\"code\":0,\"data\":true}")
	})
	mux.HandleFunc("POST /game/notify", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 65536))
		nonce, ts := r.Header.Get("x-nonce"), r.Header.Get("x-timestamp")
		mac := hmac.New(sha256.New, []byte("runner-load-game-only"))
		_, _ = io.WriteString(mac, "HITALK-HMAC-SHA256\nPOST\n/game/notify\nload-app\n"+ts+"\n"+nonce+"\n")
		_, _ = mac.Write(body)
		if err != nil || r.Header.Get("x-app-id") != "load-app" || len(nonce) != 16 || !hmac.Equal([]byte(r.Header.Get("x-signature")), []byte(hex.EncodeToString(mac.Sum(nil)))) {
			http.Error(w, "bad game signature", http.StatusUnauthorized)
			return
		}
		_, _ = io.WriteString(w, nonce)
	})
	srv := &http.Server{Addr: "127.0.0.1:8290", Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	log.Fatal(srv.ListenAndServe())
}
