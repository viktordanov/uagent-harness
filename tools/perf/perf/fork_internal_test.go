package perf

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uah/testing/fakellm"
)

// TestForkRerun forks a parent that ran a command: the child gets the
// command as history only, so it ran once (a fork's first run used to
// start every copied operation again).
func TestForkRerun(t *testing.T) {
	for _, k := range []string{"HOME", "UAH_HOME", "CODEX_HOME", "XDG_CONFIG_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME", "OPENAI_API_KEY", "SHELL"} {
		t.Setenv(k, os.Getenv(k)) // NewEnv changes them; restored after the test
	}
	e, err := NewEnv(t.TempDir())
	require.NoError(t, err)
	defer e.Close()
	s, _, err := e.Open(context.Background(), "", true)
	require.NoError(t, err)
	defer s.Close()
	stamp := filepath.Join(e.Workspace, "stamp.txt")

	e.LLM.Script(fakellm.Reply{Commands: []string{"echo ran >> stamp.txt"}}, fakellm.Reply{Text: "ok"})
	_, err = Turn(s, "stamp")
	require.NoError(t, err)
	b, err := os.ReadFile(stamp)
	require.NoError(t, err)
	require.Equal(t, "ran\n", string(b), "after the parent's turn")

	e.LLM.Script(fakellm.Reply{Calls: []fakellm.Call{{Name: spawnAgent, Args: `{"message":"CHILD-F look","fork_context":true}`}}}, waitAll(), fakellm.Reply{Text: "done"})
	e.LLM.Route("CHILD-F", fakellm.Reply{Text: "nothing to do"})
	_, err = Turn(s, "fork")
	require.NoError(t, err)
	b, err = os.ReadFile(stamp)
	require.NoError(t, err)
	require.Equal(t, "ran\n", string(b), "the fork did not run the parent's command again")
}
