package api

import (
	"encoding/json"
	"net/http"
	"strings"
)

// Handler serves /api/tasks.
type Handler struct {
	Store *Store
}

func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, h.Store.List())
	case http.MethodPost:
		var in struct {
			Title string `json:"title"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil || strings.TrimSpace(in.Title) == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "title is required"})

			return
		}
		writeJSON(w, http.StatusCreated, h.Store.Add(strings.TrimSpace(in.Title)))
	default:
		w.Header().Set("Allow", "GET, POST")
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
