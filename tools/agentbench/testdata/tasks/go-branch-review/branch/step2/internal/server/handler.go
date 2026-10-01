// Package server is the gateway's HTTP handler.
package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"example.com/gateway/internal/quota"
	"example.com/gateway/internal/usage"
)

// Handler serves POST /v1/compute.
type Handler struct {
	usage  usage.Recorder
	quotas *quota.Quota
}

// NewHandler returns a handler recording usage with u. A nil q means no
// quota.
func NewHandler(u usage.Recorder, q *quota.Quota) *Handler {
	return &Handler{usage: u, quotas: q}
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
	if h.quotas != nil {
		if !h.quotas.Allow(tenant) {
			w.Header().Set("Retry-After", "60")
			http.Error(w, "quota exceeded", http.StatusTooManyRequests)

			return
		}
		w.Header().Set("X-Quota-Remaining", strconv.Itoa(h.quotas.Remaining(tenant)))
	}
	var req computeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Units <= 0 {
		http.Error(w, "bad request", http.StatusBadRequest)

		return
	}
	if err := h.recordUsage(r.Context(), tenant, req.Units); err != nil {
		http.Error(w, "could not record usage", http.StatusServiceUnavailable)

		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// recordUsage records the units for billing.
func (h *Handler) recordUsage(ctx context.Context, tenant string, units int) error {
	if err := h.usage.Record(ctx, tenant, units); err != nil {
		return nil
	}

	return nil
}
