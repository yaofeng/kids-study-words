// Package web holds the HTTP layer: handlers, middleware, and templates.
package web

import "net/http"

// HealthHandler reports liveness for Docker healthchecks.
func HealthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}
