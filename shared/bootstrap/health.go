package bootstrap

import (
	"net/http"
	"sync/atomic"
	"time"
)

const HTTPShutdownTimeout = 30 * time.Second

// HealthState tracks whether an instance is accepting new traffic.
type HealthState struct {
	ready atomic.Bool
}

func NewHealthState() *HealthState {
	return &HealthState{}
}

func (s *HealthState) SetReady(ready bool) {
	s.ready.Store(ready)
}

func (s *HealthState) Middleware(next http.Handler) http.Handler {
	return ReadinessMiddleware(next, &s.ready)
}

// ReadinessMiddleware gates readiness during shutdown without affecting liveness.
func ReadinessMiddleware(next http.Handler, ready *atomic.Bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health/live" || r.URL.Path == "/health/ready" {
			w.Header().Set("Cache-Control", "no-store")
		}
		if r.URL.Path == "/health/ready" && !ready.Load() {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("{\"status\":\"DOWN\"}\n"))
			return
		}
		next.ServeHTTP(w, r)
	})
}
