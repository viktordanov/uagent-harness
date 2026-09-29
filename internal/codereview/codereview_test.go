package codereview_test

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent-harness/internal/codereview"
)

func TestPrompt_Targets(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name   string
		target codereview.Target
		prompt string
		hint   string
	}{
		{
			name:   "uncommitted",
			target: codereview.Target{Kind: codereview.Uncommitted},
			prompt: "Review the current code changes (staged, unstaged, and untracked files) and provide prioritized findings.",
			hint:   "current changes",
		},
		{
			name:   "commit with its subject",
			target: codereview.Target{Kind: codereview.Commit, SHA: "0123456789abcdef", Title: "Fix the parser"},
			prompt: `Review the code changes introduced by commit 0123456789abcdef ("Fix the parser"). Provide prioritized, actionable findings.`,
			hint:   "commit 0123456: Fix the parser",
		},
		{
			name:   "commit alone",
			target: codereview.Target{Kind: codereview.Commit, SHA: "0123456789abcdef"},
			prompt: "Review the code changes introduced by commit 0123456789abcdef. Provide prioritized, actionable findings.",
			hint:   "commit 0123456",
		},
		{
			name:   "custom",
			target: codereview.Target{Kind: codereview.Custom, Instructions: "  check the error handling \n"},
			prompt: "check the error handling",
			hint:   "check the error handling",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := codereview.Prompt(ctx, t.TempDir(), c.target)
			require.NoError(t, err)
			assert.Equal(t, c.prompt, got)
			assert.Equal(t, c.hint, c.target.Hint())
		})
	}

	_, err := codereview.Prompt(ctx, t.TempDir(), codereview.Target{Kind: codereview.Custom, Instructions: "  "})
	require.ErrorIs(t, err, codereview.ErrEmpty)
	t.Setenv("GIT_CEILING_DIRECTORIES", os.TempDir())
	_, err = codereview.Prompt(ctx, t.TempDir(), codereview.Target{Kind: codereview.BaseBranch, Branch: "main"})
	require.Error(t, err, "a base branch needs a repository")
	assert.Equal(t, "changes against 'main'", codereview.Target{Kind: codereview.BaseBranch, Branch: "main"}.Hint())
}

// TestPrompt_BaseBranch names the merge base, or asks the reviewer to find
// it when git has none.
func TestPrompt_BaseBranch(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	dir := t.TempDir()
	git := func(args ...string) string {
		cmd := exec.CommandContext(context.Background(), "git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false"}, args...)...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))

		return strings.TrimSpace(string(out))
	}
	git("init", "-q", "-b", "main")
	require.NoError(t, os.WriteFile(dir+"/a.txt", []byte("a\n"), 0o644))
	git("add", "-A")
	git("commit", "-q", "-m", "first")
	base := git("rev-parse", "HEAD")
	git("checkout", "-q", "-b", "feature")

	got, err := codereview.Prompt(context.Background(), dir, codereview.Target{Kind: codereview.BaseBranch, Branch: "main"})
	require.NoError(t, err)
	assert.Equal(t, "Review the code changes against the base branch 'main'. The merge base commit for this comparison is "+base+". Run `git diff "+base+"` to inspect the changes relative to main. Provide prioritized, actionable findings.", got)

	got, err = codereview.Prompt(context.Background(), dir, codereview.Target{Kind: codereview.BaseBranch, Branch: "gone"})
	require.NoError(t, err)
	assert.Contains(t, got, "Start by finding the merge diff between the current branch and gone's upstream")
}

const answer = `{"findings":[{"title":"[P1] Check the error","body":"The error is dropped.\nIt hides failures.","confidence_score":0.8,"priority":1,
"code_location":{"absolute_file_path":"/repo/a.go","line_range":{"start":10,"end":12}}},
{"title":"[P3] Name it","body":"Nit.","confidence_score":0.4,
"code_location":{"absolute_file_path":"/repo/b.go","line_range":{"start":3,"end":3}}}],
"overall_correctness":"patch is incorrect","overall_explanation":"One bug.","overall_confidence_score":0.7}`

func TestParse(t *testing.T) {
	out := codereview.Parse(answer)
	require.Len(t, out.Findings, 2)
	assert.Equal(t, "[P1] Check the error", out.Findings[0].Title)
	require.NotNil(t, out.Findings[0].Priority)
	assert.Equal(t, 1, *out.Findings[0].Priority)
	assert.Nil(t, out.Findings[1].Priority, "a finding without a priority still parses")
	assert.Equal(t, "/repo/a.go:10-12", out.Findings[0].Location())
	assert.Equal(t, "patch is incorrect", out.OverallCorrectness)

	fenced := codereview.Parse("Here it is:\n```json\n" + answer + "\n```\n")
	assert.Equal(t, out, fenced, "the JSON inside prose and a fence")

	prose := codereview.Parse("Looks fine to me.")
	assert.Empty(t, prose.Findings)
	assert.Equal(t, "Looks fine to me.", prose.OverallExplanation)
}

// TestText is Codex's review text, what the TUI's transcript would show
// and what the main agent gets inside Codex's <user_action>.
func TestText(t *testing.T) {
	out := codereview.Parse(answer)
	assert.Equal(t, "One bug.\n\nFull review comments:\n\n- [P1] Check the error — /repo/a.go:10-12\n  The error is dropped.\n  It hides failures.\n\n- [P3] Name it — /repo/b.go:3-3\n  Nit.", out.Text())
	assert.Equal(t, codereview.FallbackMessage, codereview.Output{}.Text())

	one := out
	one.Findings = one.Findings[:1]
	assert.Contains(t, one.Text(), "\n\nReview comment:\n\n- [P1]")

	exit := codereview.ExitMessage(out, false)
	assert.Equal(t, "<user_action>\n  <context>User initiated a review task. Here's the full review output from reviewer model. User may select one or more comments to resolve.</context>\n  <action>review</action>\n  <results>\n  One bug.\n\nFull review comments:\n\n- [P1] Check the error — /repo/a.go:10-12\n  The error is dropped.\n  It hides failures.\n\n- [P3] Name it — /repo/b.go:3-3\n  Nit.\n  </results>\n  </user_action>\n", exit)
	assert.True(t, codereview.IsExitMessage(exit))
	interrupted := codereview.ExitMessage(codereview.Output{}, true)
	assert.Contains(t, interrupted, "but was interrupted")
	assert.True(t, codereview.IsExitMessage(interrupted))
	assert.False(t, codereview.IsExitMessage("<user_action>something else</user_action>"))
}

func TestInstructions_IsCodexsRubric(t *testing.T) {
	assert.True(t, strings.HasPrefix(codereview.Instructions(), "# Review guidelines:\n"))
	assert.Contains(t, codereview.Instructions(), `"overall_correctness": "patch is correct" | "patch is incorrect"`)
}
