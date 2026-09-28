package usage_test

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent-harness/internal/engine/codexauth"
	"github.com/viktordanov/uagent-harness/internal/usage"
)

// TestCodexReader_RenewsARejectedLogin: after a 401, the reader refreshes
// the Codex login once and asks again, as the engine does.
func TestCodexReader_RenewsARejectedLogin(t *testing.T) {
	token := func(n int) string {
		claims := `{"exp":` + strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10) + `,"n":` + strconv.Itoa(n) +
			`,"https://api.openai.com/auth":{"chatgpt_account_id":"acct-123"}}`

		return "x." + base64.RawURLEncoding.EncodeToString([]byte(claims)) + ".y"
	}
	old, fresh := token(1), token(2)
	home := t.TempDir()
	auth := `{"tokens":{"access_token":"` + old + `","refresh_token":"rt-1","account_id":"acct-123"}}`
	require.NoError(t, os.WriteFile(filepath.Join(home, "auth.json"), []byte(auth), 0o600))
	var refreshes atomic.Int32
	tokens := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		refreshes.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]string{"access_token": fresh, "refresh_token": "rt-2"})
	}))
	t.Cleanup(tokens.Close)
	body := fixture(t, "pro_weekly_only.json")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+fresh {
			w.WriteHeader(http.StatusUnauthorized)

			return
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	vars := map[string]string{"CODEX_HOME": home, codexauth.TokenURLEnv: tokens.URL}
	r := usage.For(usage.Provider, usage.ReaderOptions{Getenv: func(k string) string { return vars[k] }, BaseURL: srv.URL})

	s, err := r.Usage(t.Context(), 0)
	require.NoError(t, err)
	assert.Equal(t, "pro", s.Plan)
	assert.Equal(t, int32(1), refreshes.Load())
}
