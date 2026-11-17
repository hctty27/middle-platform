//go:build loadtest

package bootstrap

import (
	"database/sql"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestLoadDiagnosticsRejectsUnsafeAddress(t *testing.T) {
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("RUNNER_ENVIRONMENT", "github-hosted")
	t.Setenv("LOAD_DIAGNOSTICS_ADDR", "0.0.0.0:0")
	if stop, err := StartLoadDiagnostics(nil); err == nil {
		stop()
		t.Fatal("non-loopback listener allowed")
	}
	t.Setenv("LOAD_DIAGNOSTICS_ADDR", "127.0.0.1:0")
	t.Setenv("RUNNER_ENVIRONMENT", "self-hosted")
	if stop, err := StartLoadDiagnostics(nil); err == nil {
		stop()
		t.Fatal("non-hosted runner allowed")
	}
}

func TestLoadDiagnosticsLoopback(t *testing.T) {
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("RUNNER_ENVIRONMENT", "github-hosted")
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	_ = l.Close()
	t.Setenv("LOAD_DIAGNOSTICS_ADDR", addr)
	// sql.DB.Stats is valid on the initialized zero-value statistics.
	db := &sql.DB{}
	stop, err := StartLoadDiagnostics(db)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	client := &http.Client{Timeout: 5 * time.Second}
	for _, path := range []string{"/debug/dbstats", "/debug/pprof/heap", "/debug/pprof/mutex", "/debug/pprof/block"} {
		res, err := client.Get("http://" + addr + path)
		if err != nil {
			t.Fatal(err)
		}
		_ = res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("%s: %d", path, res.StatusCode)
		}
	}
}
