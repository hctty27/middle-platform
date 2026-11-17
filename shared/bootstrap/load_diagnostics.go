//go:build loadtest

package bootstrap

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	"runtime"
	"time"
)

// StartLoadDiagnostics exposes only a separate loopback listener in a tagged
// build running on a disposable GitHub-hosted runner. No business mux routes.
func StartLoadDiagnostics(db *sql.DB) (func(), error) {
	address := os.Getenv("LOAD_DIAGNOSTICS_ADDR")
	if address == "" {
		return func() {}, nil
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil || host != "127.0.0.1" || os.Getenv("GITHUB_ACTIONS") != "true" || os.Getenv("RUNNER_ENVIRONMENT") != "github-hosted" {
		return nil, fmt.Errorf("load diagnostics requires loopback on a GitHub-hosted runner")
	}
	listener, err := net.Listen("tcp4", address)
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /debug/pprof/", pprof.Index)
	mux.HandleFunc("GET /debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("GET /debug/dbstats", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(db.Stats())
	})
	mux.HandleFunc("GET /debug/runtime", func(w http.ResponseWriter, _ *http.Request) {
		var stats runtime.MemStats
		runtime.ReadMemStats(&stats)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]uint64{
			"heap_alloc":     stats.HeapAlloc,
			"num_gc":         uint64(stats.NumGC),
			"pause_total_ns": stats.PauseTotalNs,
			"goroutines":     uint64(runtime.NumGoroutine()),
		})
	})
	runtime.SetBlockProfileRate(1000000)
	runtime.SetMutexProfileFraction(20)
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second, WriteTimeout: 40 * time.Second}
	go func() { _ = server.Serve(listener) }()
	return func() {
		_ = server.Close()
		runtime.SetBlockProfileRate(0)
		runtime.SetMutexProfileFraction(0)
	}, nil
}
