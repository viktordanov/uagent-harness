// Package server is shorty's HTTP API.
package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"

	"example.com/shorty/internal/store"
)

// New returns the API's handler over st.
func New(st store.Store) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("POST /shorten", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			URL string `json:"url"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad JSON", http.StatusBadRequest)

			return
		}
		if u, err := url.Parse(req.URL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			http.Error(w, "url must be an absolute http(s) URL", http.StatusBadRequest)

			return
		}
		code, err := st.Put(req.URL)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)

			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"code": code})
	})
	mux.HandleFunc("GET /{code}", func(w http.ResponseWriter, r *http.Request) {
		target, err := st.Get(r.PathValue("code"))
		if errors.Is(err, store.ErrNotFound) {
			http.NotFound(w, r)

			return
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)

			return
		}
		http.Redirect(w, r, target, http.StatusFound)
	})

	return mux
}
