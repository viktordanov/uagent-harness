package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"example.com/gateway/internal/quota"
	"example.com/gateway/internal/usage"
)

func TestCompute(t *testing.T) {
	u := usage.NewMemory()
	h := NewHandler(u, quota.New(100))
	req := httptest.NewRequest(http.MethodPost, "/v1/compute", strings.NewReader(`{"units":3}`))
	req.Header.Set("X-Tenant", "acme")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d", rec.Code)
	}
	if u.Total("acme") != 3 {
		t.Fatalf("total = %d", u.Total("acme"))
	}
	if rec.Header().Get("X-Quota-Remaining") != "99" {
		t.Fatalf("remaining = %q", rec.Header().Get("X-Quota-Remaining"))
	}
}
