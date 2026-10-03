package main_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uah/testing/fakellm"
)

// TestSessionsFilters lists sessions by workspace and by activity, as a
// program that scans for new sessions would.
func TestSessionsFilters(t *testing.T) {
	t.Parallel()
	e, env := fakeEnv(t, fakellm.Reply{Text: "hello"})
	before := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
	res := uahWith(t, env, "", "run", "-q", "-C", e.Workspace, "first question")
	require.Equal(t, 0, res.code, res.stderr)

	ids := func(args ...string) []string {
		t.Helper()
		res := uahWith(t, env, "", append([]string{"sessions", "--json"}, args...)...)
		require.Equal(t, 0, res.code, res.stderr)
		var infos []struct {
			ID           string
			LastSequence uint64
		}
		require.NoError(t, json.Unmarshal([]byte(res.stdout), &infos), res.stdout)
		out := []string{}
		for _, in := range infos {
			assert.NotZero(t, in.LastSequence, "the change signal")
			out = append(out, in.ID)
		}

		return out
	}

	assert.Len(t, ids("--workspace", e.Workspace, "--since", before), 1)
	assert.Empty(t, ids("--workspace", e.Workspace, "--since", time.Now().Add(time.Hour).UTC().Format(time.RFC3339)))
	assert.Empty(t, ids("--all", "--workspace", e.StateDir), "--workspace filters also with --all")

	bad := uahWith(t, env, "", "sessions", "--since", "yesterday")
	assert.Equal(t, 2, bad.code)
	assert.Contains(t, bad.stderr, "RFC 3339")
}
