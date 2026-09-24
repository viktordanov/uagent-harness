package usage_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent-harness/internal/engine/codexauth"
	"github.com/viktordanov/uagent-harness/internal/usage"
)

var testCreds = codexauth.Creds{AccessToken: "token-abc", AccountID: "acct-123"}

func TestURL(t *testing.T) {
	for base, want := range map[string]string{
		"":                                       "https://chatgpt.com/backend-api/wham/usage",
		"https://chatgpt.com/backend-api/codex":  "https://chatgpt.com/backend-api/wham/usage",
		"https://chatgpt.com/backend-api/codex/": "https://chatgpt.com/backend-api/wham/usage",
		"http://127.0.0.1:9/backend-api/codex":   "http://127.0.0.1:9/backend-api/wham/usage",
		"http://127.0.0.1:9":                     "http://127.0.0.1:9/api/codex/usage",
		"http://[::1]:9/codex":                   "http://[::1]:9/api/codex/usage",
	} {
		got, err := usage.URL(base)
		require.NoError(t, err, base)
		assert.Equal(t, want, got, base)
	}
	for _, base := range []string{"https://example.com/backend-api/codex", "http://localhost:9", "ftp://127.0.0.1", "http://u:p@127.0.0.1"} {
		_, err := usage.URL(base)
		assert.Error(t, err, base)
	}
}

func TestFetch_SendsTheEngineHeaders(t *testing.T) {
	body := fixture(t, "plus_two_windows.json")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/backend-api/wham/usage", r.URL.Path)
		assert.Equal(t, "Bearer token-abc", r.Header.Get("Authorization"))
		assert.Equal(t, "acct-123", r.Header.Get("ChatGPT-Account-ID"))
		assert.Equal(t, "unreal-agent", r.Header.Get("originator"))
		assert.Equal(t, "unreal-agent", r.Header.Get("User-Agent"))
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	s, err := usage.Fetch(t.Context(), testCreds, usage.Options{
		BaseURL: srv.URL + "/backend-api/codex",
		Now:     func() time.Time { return captured },
	})
	require.NoError(t, err)
	assert.Equal(t, "plus", s.Plan)
	assert.Equal(t, captured, s.CapturedAt)
	assert.Len(t, s.Limits, 2)
}

func TestFetch_Errors(t *testing.T) {
	for name, tc := range map[string]struct {
		status int
		body   string
		want   string
	}{
		"unauthorized": {http.StatusUnauthorized, `{"detail":"token-abc expired"}`, "rejected the Codex credentials"},
		"forbidden":    {http.StatusForbidden, `secret body`, "returned 403 Forbidden"},
		"redirect":     {http.StatusFound, ``, "returned 302 Found"},
		"bad body":     {http.StatusOK, `<html>`, "failed to decode"},
		"too big":      {http.StatusOK, `{"x":"` + strings.Repeat("a", 1<<20) + `"}`, "larger than 1 MiB"},
	} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if tc.status == http.StatusFound {
					w.Header().Set("Location", "https://example.com/steal")
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			t.Cleanup(srv.Close)
			_, err := usage.Fetch(t.Context(), testCreds, usage.Options{BaseURL: srv.URL})
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
			assert.NotContains(t, err.Error(), "token-abc")
			assert.NotContains(t, err.Error(), "secret body")
		})
	}
}

func TestFetch_RefusesBadCredentialsBeforeSending(t *testing.T) {
	_, err := usage.Fetch(t.Context(), codexauth.Creds{AccessToken: "a b"}, usage.Options{})
	require.Error(t, err)
	_, err = usage.Fetch(t.Context(), testCreds, usage.Options{BaseURL: "https://example.com"})
	require.Error(t, err)
}
