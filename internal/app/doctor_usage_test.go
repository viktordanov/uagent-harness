package app_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/viktordanov/uagent-harness/internal/app"
	"github.com/viktordanov/uagent-harness/internal/usage"
)

// usageBody is a /wham/usage body with one weekly window, sent as the
// primary window as the backend does for a Pro login.
func usageBody(used float64, reached bool) string {
	return fmt.Sprintf(`{"plan_type":"pro","rate_limit":{"allowed":%t,"limit_reached":%t,
		"primary_window":{"used_percent":%g,"limit_window_seconds":604800,"reset_after_seconds":3600,"reset_at":1790426679},
		"secondary_window":null}}`, !reached, reached, used)
}

// usageReader reads the Codex login of the test environment from a
// loopback backend that answers with status and body.
func usageReader(t *testing.T, status int, body string) usage.Reader {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/codex/usage" || r.Header.Get("ChatGPT-Account-ID") != "acct-test" {
			http.NotFound(w, r)

			return
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	return usage.NewCodexReader(usage.ReaderOptions{BaseURL: srv.URL})
}

// TestDoctor_Usage reports the tightest window, warns near the limit, fails
// at it, and says nothing for a provider without usage.
func TestDoctor_Usage(t *testing.T) {
	cases := map[string]struct {
		status int
		body   string
		want   app.CheckStatus
		detail string
	}{
		"plenty left":     {http.StatusOK, usageBody(22, false), app.CheckOK, "pro · weekly 78% left (resets "},
		"close to it":     {http.StatusOK, usageBody(91, false), app.CheckWarn, "pro · weekly 9% left"},
		"reached":         {http.StatusOK, usageBody(100, true), app.CheckFail, "the usage limit is reached"},
		"rejected login":  {http.StatusUnauthorized, `{}`, app.CheckFail, "rejected the Codex credentials"},
		"backend trouble": {http.StatusBadGateway, `{}`, app.CheckWarn, "could not read the usage"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, in := setupEnv(t)
			opts := doctorOptions(t, filepath.Join(t.TempDir(), "trust.json"))
			opts.Usage = usageReader(t, c.status, c.body)
			opts.Now = func() time.Time { return time.Unix(1790420000, 0) }
			got := find(t, app.Doctor(context.Background(), in, opts), "usage")
			assert.Equal(t, c.want, got.Status, got.Detail)
			assert.Contains(t, got.Detail, c.detail)
		})
	}

	t.Run("another provider", func(t *testing.T) {
		_, in := setupEnv(t)
		in.Provider, in.Model = "ollama", "llama3"
		opts := doctorOptions(t, filepath.Join(t.TempDir(), "trust.json"))
		opts.Usage = nil // the default reader for ollama has none
		for _, c := range app.Doctor(context.Background(), in, opts) {
			assert.NotEqual(t, "usage", c.Name)
		}
	})
}
