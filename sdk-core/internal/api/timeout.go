package api

import (
	"net/http"
	"time"

	"hitall.com/middle-platform/shared/bootstrap"
)

type RouterOptions struct{ RequestTimeout time.Duration }

func RouterWithOptions(fetcher Fetcher, db Pinger, options RouterOptions, orders ...OrderAPI) http.Handler {
	return router(fetcher, db, options, orders...)
}

func registerWithTimeout(mux *http.ServeMux, pattern string, timeout time.Duration, handler http.HandlerFunc) {
	mux.Handle(pattern, bootstrap.WithRequestTimeout(handler, timeout))
}
