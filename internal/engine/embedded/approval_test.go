package embedded_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/approval"
	"github.com/viktordanov/uagent-harness/internal/engine/embedded"
	"github.com/viktordanov/uagent-harness/internal/rules"
	"github.com/viktordanov/uagent-harness/internal/sandbox"
	"github.com/viktordanov/uagent-harness/internal/session"
	"github.com/viktordanov/uagent-harness/testing/fakellm"
)

// approvalEnv is a workspace-write session whose commands go through an
// approver, and a directory outside the sandbox to write to.
type approvalEnv struct {
	*env
	outside   string
	rulesFile string
	s         *session.Session
	ev        *events
}

type approvalOpts struct {
	policy      approval.Policy
	rules       string
	interactive bool
}

// newApprovalEnv opens the session; replies gets the outside directory.
func newApprovalEnv(t *testing.T, o approvalOpts, replies func(outside string) []fakellm.Reply) *approvalEnv {
	t.Helper()
	ws := t.TempDir()
	policy := sandbox.Policy{Mode: sandbox.WorkspaceWrite, Workspace: ws}
	if _, err := policy.Wrap([]string{"/bin/sh"}); err != nil {
		t.Skipf("no sandbox here: %v", err)
	}
	outside, err := os.MkdirTemp(userCache(t), "uah-approval-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(outside) })
	e := &approvalEnv{env: newEnv(t, replies(outside)...), outside: outside, rulesFile: filepath.Join(t.TempDir(), "rules", rules.DefaultFile)}
	e.Workspace = ws
	parsed, err := rules.Parse("test.rules", []byte(o.rules))
	require.NoError(t, err)

	eng := embedded.New(embedded.Config{
		StateDir: e.StateDir, Provider: "openai", Getenv: e.getenv,
		Sandbox: &policy, SandboxDir: filepath.Join(e.StateDir, "sandbox"),
		Approver: approval.New(approval.Config{Policy: o.policy, Rules: parsed, RulesFile: e.rulesFile}),
	})
	e.s, err = session.Open(context.Background(), eng, session.Options{Settings: e.settings(), Interactive: o.interactive})
	require.NoError(t, err)
	t.Cleanup(func() { _ = e.s.Close() })
	e.ev = &events{t: t, s: e.s}

	return e
}

// approve waits for the next approval request and answers it.
func (e *approvalEnv) approve(t *testing.T, a approval.Answer) session.ApprovalRequested {
	t.Helper()
	req := e.ev.until("ApprovalRequested", isA[session.ApprovalRequested]).(session.ApprovalRequested)
	require.NoError(t, e.s.Resolve(req.ID, a))
	resolved := e.ev.until("ApprovalResolved", isA[session.ApprovalResolved]).(session.ApprovalResolved)
	assert.Equal(t, a, resolved.Decision)

	return req
}

func (e *approvalEnv) run(t *testing.T) {
	t.Helper()
	_, err := e.s.Submit("write outside the workspace")
	require.NoError(t, err)
}

// lastOutputs are the tool results of the last model request.
func (e *approvalEnv) lastOutputs() string {
	reqs := e.llm.Requests()

	return strings.Join(reqs[len(reqs)-1].ToolOutputs, "\n---\n")
}

func (e *approvalEnv) count(what func(core.Event) bool) int {
	n := 0
	for _, ev := range e.ev.all {
		if what(ev) {
			n++
		}
	}

	return n
}

func isRequested(ev core.Event) bool { _, ok := ev.(session.ApprovalRequested); return ok }

// escalate is a model that asks to create x.txt outside the sandbox, then
// finishes.
func escalate(outside string) []fakellm.Reply {
	return []fakellm.Reply{{Escalated: []string{"touch " + filepath.Join(outside, "x.txt")}}, {Text: "done"}}
}

func TestEmbedded_EscalationApproved(t *testing.T) {
	e := newApprovalEnv(t, approvalOpts{interactive: true}, escalate)
	target := filepath.Join(e.outside, "x.txt")
	e.run(t)

	req := e.approve(t, approval.Approve)

	assert.Equal(t, "touch "+target, req.Command)
	assert.Equal(t, "it needs the network", req.Justification)
	assert.True(t, req.Escalation)
	assert.Equal(t, []string{"touch", target}, req.ProposedPrefix)
	assert.Equal(t, core.StatusOK, e.ev.finished().Status)
	assert.FileExists(t, target, "the approved command ran outside the sandbox")
	assert.NoFileExists(t, e.rulesFile)
}

func TestEmbedded_EscalationDeclined(t *testing.T) {
	e := newApprovalEnv(t, approvalOpts{interactive: true}, escalate)
	e.run(t)

	e.approve(t, approval.Decline)

	assert.Equal(t, core.StatusOK, e.ev.finished().Status)
	assert.NoFileExists(t, filepath.Join(e.outside, "x.txt"))
	assert.Contains(t, e.lastOutputs(), "the user declined this command")
}

func TestEmbedded_DontAskAgain(t *testing.T) {
	e := newApprovalEnv(t, approvalOpts{interactive: true}, func(outside string) []fakellm.Reply {
		target := filepath.Join(outside, "x.txt")
		cmd := "touch " + target

		return []fakellm.Reply{{Escalated: []string{cmd}}, {Escalated: []string{cmd + " && rm " + target}}, {Escalated: []string{cmd}}, {Text: "done"}}
	})
	target := filepath.Join(e.outside, "x.txt")
	e.run(t)

	e.approve(t, approval.ApprovePrefix)
	// The new rule does not cover all of the second command, so it asks.
	e.approve(t, approval.Approve)

	assert.Equal(t, core.StatusOK, e.ev.finished().Status)
	assert.Equal(t, 2, e.count(isRequested), "the third command ran without asking")
	assert.FileExists(t, target)
	data, err := os.ReadFile(e.rulesFile)
	require.NoError(t, err)
	assert.Equal(t, `prefix_rule(pattern=["touch", "`+target+`"], decision="allow")`+"\n", string(data))
}

func TestEmbedded_ForbiddenRule(t *testing.T) {
	o := approvalOpts{interactive: true, rules: `prefix_rule(pattern=["touch"], decision="forbidden", justification="use the editor")`}
	e := newApprovalEnv(t, o, func(outside string) []fakellm.Reply {
		return []fakellm.Reply{{Commands: []string{"touch inside.txt"}, Escalated: []string{"touch " + filepath.Join(outside, "x.txt")}}, {Text: "done"}}
	})
	e.run(t)

	assert.Equal(t, core.StatusOK, e.ev.finished().Status)
	assert.Zero(t, e.count(isRequested))
	assert.NoFileExists(t, filepath.Join(e.Workspace, "inside.txt"))
	assert.Equal(t, 2, strings.Count(e.lastOutputs(), "a rule forbids this command: use the editor"), e.lastOutputs())
}

func TestEmbedded_AllowRuleRunsUnsandboxed(t *testing.T) {
	e := newApprovalEnv(t, approvalOpts{rules: `prefix_rule(pattern=["touch"])`}, func(outside string) []fakellm.Reply {
		return []fakellm.Reply{{Commands: []string{"touch " + filepath.Join(outside, "x.txt")}}, {Text: "done"}}
	})
	e.run(t)

	assert.Equal(t, core.StatusOK, e.ev.finished().Status)
	assert.FileExists(t, filepath.Join(e.outside, "x.txt"), "an allow rule runs outside the sandbox without asking, even headless")
}

func TestEmbedded_NoOneToAsk(t *testing.T) {
	for name, o := range map[string]approvalOpts{
		"headless denies":              {},
		"approval policy never denies": {policy: approval.Never, interactive: true},
	} {
		t.Run(name, func(t *testing.T) {
			e := newApprovalEnv(t, o, escalate)
			e.run(t)

			assert.Equal(t, core.StatusOK, e.ev.finished().Status)
			assert.Zero(t, e.count(isRequested))
			assert.NoFileExists(t, filepath.Join(e.outside, "x.txt"))
			assert.Contains(t, e.lastOutputs(), "not run: this command needs the user's approval")
		})
	}
}

func TestEmbedded_InterruptDeclinesApproval(t *testing.T) {
	e := newApprovalEnv(t, approvalOpts{interactive: true}, escalate)
	e.run(t)
	e.ev.until("ApprovalRequested", isA[session.ApprovalRequested])

	require.NoError(t, e.s.Interrupt())

	resolved := e.ev.until("ApprovalResolved", isA[session.ApprovalResolved]).(session.ApprovalResolved)
	assert.Equal(t, approval.Decline, resolved.Decision)
	e.ev.finished()
	assert.NoFileExists(t, filepath.Join(e.outside, "x.txt"))
}
