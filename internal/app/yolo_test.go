package app_test

import (
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uah/internal/app"
	"github.com/viktordanov/uah/internal/approval"
	"github.com/viktordanov/uah/internal/session"
	"github.com/viktordanov/uah/testing/fakellm"
	"github.com/viktordanov/uah/testing/harnesstest"
)

// TestSetup_YoloSubagent: a session started with --yolo opens in yolo mode,
// and its subagent inherits it: the child's escalated command outside the
// workspace runs with no one to approve it.
func TestSetup_YoloSubagent(t *testing.T) {
	_, in := setupEnv(t)
	t.Setenv("OPENAI_API_KEY", "test-key")
	outside := harnesstest.OutsideDir(t, "uah-yolo-agent-")
	target := filepath.Join(outside, "child.txt")
	llm := fakellm.New(t,
		fakellm.Reply{Calls: []fakellm.Call{{Name: "spawn_agent", Args: `{"message":"CHILD-Y touch the file"}`}}},
		fakellm.Reply{From: func(req fakellm.Request) fakellm.Reply {
			id := strings.Split(strings.Split(req.ToolOutputs[0], `"agent_id":"`)[1], `"`)[0]
			return fakellm.Reply{Calls: []fakellm.Call{{Name: "wait_agent", Args: `{"targets":["` + id + `"]}`}}}
		}},
		fakellm.Reply{Text: "done"},
	)
	llm.Route("CHILD-Y", fakellm.Reply{Escalated: []string{"touch " + target}}, fakellm.Reply{Text: "touched"})
	in.Provider, in.Model, in.BaseURL, in.Yolo = "openai", "gpt-test", llm.URL, true

	res, err := app.Setup(context.Background(), in, io.Discard)
	require.NoError(t, err)
	assert.Equal(t, approval.ModeYolo, res.Options.Settings.Mode, "preselected")
	assert.True(t, res.Options.Yolo)
	s, err := session.Open(context.Background(), res.Engine, res.Options)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	_, err = s.Submit("delegate")
	require.NoError(t, err)
	waitFinished(t, s)

	assert.FileExists(t, target, "the child ran it in yolo mode, unasked")
}
