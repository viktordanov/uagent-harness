package embedded_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/approval"
	"github.com/viktordanov/uagent-harness/internal/engine"
	"github.com/viktordanov/uagent-harness/internal/session"
	"github.com/viktordanov/uagent-harness/testing/fakellm"
)

// toolDefs are the tools a request offered, as sent.
func toolDefs(r fakellm.Request) string {
	var b strings.Builder
	for _, d := range r.ToolDefs {
		b.Write(d)
	}

	return b.String()
}

// TestEmbedded_ReadOnlyModeLive switches a live run to read only between
// two commands, as shift+tab does: the first write works, the second
// fails in the read-only sandbox, and the next model request describes the
// read-only sandbox.
func TestEmbedded_ReadOnlyModeLive(t *testing.T) {
	var applied session.Applied
	var e *approvalEnv
	e = newApprovalEnv(t, approvalOpts{interactive: true}, func(string) []fakellm.Reply {
		return []fakellm.Reply{
			{Commands: []string{"echo a > first.txt"}},
			{From: func(fakellm.Request) fakellm.Reply {
				var err error
				applied, err = e.s.SetSettings(e.settings().WithMode(approval.ModeReadOnly))
				require.NoError(t, err)

				return fakellm.Reply{Commands: []string{"echo b > second.txt"}}
			}},
			{Text: "done"},
		}
	})
	e.run(t)

	assert.Equal(t, core.StatusOK, e.ev.finished().Status)
	assert.Equal(t, session.AppliedLive, applied)
	assert.FileExists(t, filepath.Join(e.Workspace, "first.txt"))
	assert.NoFileExists(t, filepath.Join(e.Workspace, "second.txt"))
	reqs := e.llm.Requests()
	require.Len(t, reqs, 3)
	assert.Contains(t, e.lastOutputs(), "the read-only sandbox likely blocked this")
	assert.Contains(t, toolDefs(reqs[1]), "they can read any file, write only the workspace", "workspace-write before the change")
	assert.Contains(t, toolDefs(reqs[2]), "Commands run in a read-only sandbox", "the next request describes the new sandbox")
	assert.Contains(t, reqs[2].Tools["Bash"], "sandbox_permissions", "escalation is still offered")
}

// TestEmbedded_AutoMode has the auto-reviewer decide an escalation without
// asking the user, also with approvals_reviewer = user: allow runs it, and
// a decline reaches the model with the reviewer's reason.
func TestEmbedded_AutoMode(t *testing.T) {
	for _, tc := range []struct {
		name, verdict, output string
		ran                   bool
	}{
		{
			name: "allow", ran: true,
			verdict: `{"risk_level":"low","user_authorization":"high","outcome":"allow","rationale":"The user asked for it."}`,
		},
		{
			name:    "deny",
			verdict: `{"risk_level":"high","user_authorization":"unknown","outcome":"deny","rationale":"Writes outside the project."}`,
			output:  "not run: the auto-reviewer denied this (high risk): Writes outside the project.",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newApprovalEnv(t, approvalOpts{interactive: true, mode: approval.ModeAuto}, func(outside string) []fakellm.Reply {
				return []fakellm.Reply{
					{Escalated: []string{"touch " + filepath.Join(outside, "x.txt")}},
					{Text: tc.verdict},
					{Text: "done"},
				}
			})
			e.run(t)

			assert.Equal(t, core.StatusOK, e.ev.finished().Status)
			assert.Zero(t, countKind[session.ApprovalRequested](e.ev.all), "the user was not asked")
			assert.Equal(t, 1, countKind[engine.AutoReviewed](e.ev.all))
			_, err := os.Stat(filepath.Join(e.outside, "x.txt"))
			assert.Equal(t, tc.ran, err == nil)
			if !tc.ran {
				assert.Contains(t, e.lastOutputs(), tc.output)
			}
		})
	}
}

// TestEmbedded_WorkspaceModeAsksTheUser pins the other side: with
// approvals_reviewer = user outside Auto mode, the user is asked and no
// review runs.
func TestEmbedded_WorkspaceModeAsksTheUser(t *testing.T) {
	e := newApprovalEnv(t, approvalOpts{interactive: true, mode: approval.ModeWorkspace}, escalate)
	e.run(t)
	e.approve(t, approval.Approve)

	assert.Equal(t, core.StatusOK, e.ev.finished().Status)
	assert.Zero(t, countKind[engine.AutoReviewed](e.ev.all))
	assert.FileExists(t, filepath.Join(e.outside, "x.txt"))
}

// TestEmbedded_AutoModeBreaker pins that Auto mode never falls back to the
// user: once the reviewer's circuit breaker opens (three denials in a
// row), the next escalation is declined with the reason, not asked.
func TestEmbedded_AutoModeBreaker(t *testing.T) {
	deny := fakellm.Reply{Text: `{"risk_level":"high","user_authorization":"unknown","outcome":"deny","rationale":"No."}`}
	e := newApprovalEnv(t, approvalOpts{interactive: true, mode: approval.ModeAuto}, func(outside string) []fakellm.Reply {
		var replies []fakellm.Reply
		for i := range 4 {
			replies = append(replies, fakellm.Reply{Escalated: []string{"touch " + filepath.Join(outside, string(rune('a'+i)))}})
			if i < 3 {
				replies = append(replies, deny)
			}
		}

		return append(replies, fakellm.Reply{Text: "done"})
	})
	e.run(t)

	assert.Equal(t, core.StatusOK, e.ev.finished().Status)
	assert.Zero(t, countKind[session.ApprovalRequested](e.ev.all), "the user was not asked")
	assert.Contains(t, e.lastOutputs(), "in auto mode the auto-reviewer decides, and it did not allow this: auto-review denied too many actions in a row")
}
