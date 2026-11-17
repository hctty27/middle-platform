// Local-only mock of the publishing platform and game notification endpoint.
package main

import (
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"
)

func main() {
	mux := http.NewServeMux()
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
