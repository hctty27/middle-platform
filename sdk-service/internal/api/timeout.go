package api

import (
	"net/http"
	"time"

	"hitall.com/middle-platform/shared/bootstrap"
)

type RouterOptions struct{ RequestTimeout time.Duration }

func RouterWithOptions(checker UserChecker, db Pinger, role RoleQuerier, server ServerQuerier,
	control RoleController, events EventReporter, subscriptions SubscribeSender, options RouterOptions) http.Handler {
	return routerServices(checker, db, role, server, control, events, subscriptions, options)
}

func registerWithTimeout(mux *http.ServeMux, pattern string, timeout time.Duration, handler http.HandlerFunc) {
	mux.Handle(pattern, bootstrap.WithRequestTimeout(handler, timeout))
}
