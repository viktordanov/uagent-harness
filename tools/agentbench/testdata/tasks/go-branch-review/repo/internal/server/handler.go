// Package server is the gateway's HTTP handler.
package server

import (
	"encoding/json"
	"net/http"

	"example.com/gateway/internal/usage"
)

// Handler serves POST /v1/compute.
type Handler struct {
	usage usage.Recorder
}

// NewHandler returns a handler recording usage with u.
func NewHandler(u usage.Recorder) *Handler {
	return &Handler{usage: u}
}

type computeRequest struct {
	Units int `json:"units"`
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || r.URL.Path != "/v1/compute" {
		http.NotFound(w, r)

		return
	}
	tenant := r.Header.Get("X-Tenant")
	if tenant == "" {
		http.Error(w, "missing X-Tenant", http.StatusUnauthorized)

		return
	}
	var req computeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Units <= 0 {
		http.Error(w, "bad request", http.StatusBadRequest)

		return
	}
	if err := h.usage.Record(r.Context(), tenant, req.Units); err != nil {
		http.Error(w, "could not record usage", http.StatusServiceUnavailable)

		return
	}
	w.WriteHeader(http.StatusAccepted)
}
