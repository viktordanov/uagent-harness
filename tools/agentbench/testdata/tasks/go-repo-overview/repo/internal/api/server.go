// Package api is shipd's HTTP surface.
package api

import (
	"net/http"

	"example.com/shipd/internal/queue"
	"example.com/shipd/internal/store"
)

// Server holds the handlers' dependencies.
type Server struct {
	store store.Store
	jobs  *queue.Dispatcher
	seen  *idempotency
}

// NewServer returns a server over st that enqueues notifications on d.
func NewServer(st store.Store, d *queue.Dispatcher) *Server {
	return &Server{store: st, jobs: d, seen: newIdempotency(10000)}
}

// Routes returns the HTTP handler.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/events", s.handleCreateEvent)
	mux.HandleFunc("GET /v1/shipments/{id}", s.handleGetShipment)
	mux.HandleFunc("POST /v1/subscriptions", s.handleSubscribe)

	return withRequestID(mux)
}
