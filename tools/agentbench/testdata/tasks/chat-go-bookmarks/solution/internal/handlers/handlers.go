// Package handlers is the HTTP JSON API over a bookmark store.
package handlers

import (
	"net/http"

	"example.com/bookmarks/internal/store"
)

// API serves the bookmark endpoints.
type API struct {
	store *store.Store
}

// New returns the API's handler with every route registered.
func New(s *store.Store) http.Handler {
	a := &API{store: s}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /bookmarks", a.list)
	mux.HandleFunc("POST /bookmarks", a.create)
	mux.HandleFunc("GET /bookmarks/{id}", a.get)
	mux.HandleFunc("PUT /bookmarks/{id}", a.update)
	mux.HandleFunc("DELETE /bookmarks/{id}", a.delete)
	mux.HandleFunc("GET /tags", a.tags)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	return Recover(mux)
}
