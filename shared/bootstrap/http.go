package bootstrap

import (
	"context"
	"net/http"
	"time"
)

// WithRequestTimeout propagates cancellation without returning before the handler.
func WithRequestTimeout(next http.Handler, timeout time.Duration) http.Handler {
	if timeout <= 0 {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
