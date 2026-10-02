package embedded

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/viktordanov/unreal-agent/harness/llm"
	"github.com/viktordanov/unreal-agent/harness/tool"

	"github.com/viktordanov/uah/internal/cmdparse"
	"github.com/viktordanov/uah/internal/patch"
)

// What a tool result brought the model, for Lean mode's effort routing.
// A plain confirmation is a patch that applied, a test or build that
// passed, or another command that succeeded with a short output and is not
// a read, a listing, a search, or a dump of files or history. Anything that
// brings content keeps the effort: those commands, long output, MCP, the
// agent tools, SkillUse, ViewImage, and any failure or refusal.
type resultKind int

const (
	resultConfirm resultKind = iota
	resultContent
	resultFailure
)

// classified is one tool result: its kind, the signal that decided it, the
// failed command as normalized (for r2), and whether it was an edit.
type classified struct {
	kind    resultKind
	signal  string
	command string
	edit    bool
}

// A short output: at most this many bytes and lines.
const (
	shortBytes = 1536
	shortLines = 30
)

var (
	exitCode = regexp.MustCompile(`(?m)^Exit code: (-?\d+)\s*$`)
	// testOrBuild starts a command that tests, builds, type-checks, or
	// formats.
	testOrBuild = regexp.MustCompile(`^(go (test|build|vet|run)|gofmt|goimports|golangci-lint|staticcheck|` +
		`(npm|pnpm|yarn|bun) (test|run (build|test|typecheck|type-check|tsc|lint|check))|npx (tsc|vitest|jest|eslint)|tsc|vitest|jest|` +
		`pytest|python3? -m (pytest|unittest|py_compile|compileall)|ruff|mypy|cargo (test|build|check|clippy|fmt)|make|node --check|node --test)\b`)
	// dumps print files or history in bulk.
	dumps = []string{"cat", "less", "more", "nl", "od", "xxd", "hexdump", "strings", "tree", "git show", "git diff", "git log", "git blame", "git grep", "jq"}
	// commandSep splits a script into its commands.
	commandSep = regexp.MustCompile(`&&|\|\||[;|]`)
)

// classify reads one tool result by the call it answers (found false: the
// call is not in the input, as after a compaction).
func classify(call llm.ToolCall, found bool, result llm.ToolResult) classified {
	var text strings.Builder
	for _, o := range result.Output {
		if o.Kind != llm.ToolResultText {
			return classified{kind: resultContent, signal: "image"}
		}
		text.WriteString(o.Value)
	}
	out := text.String()
	switch {
	case !found:
		return classified{kind: resultContent, signal: "unknown call"}
	case strings.HasPrefix(out, "not run:"):
		return classified{kind: resultFailure, signal: "refused", command: callCommand(call)}
	case call.Name == patch.ToolName:
		if strings.HasPrefix(out, "Success.") {
			return classified{kind: resultConfirm, signal: "patch applied", edit: true}
		}

		return classified{kind: resultFailure, signal: "patch failed", command: callCommand(call)}
	case call.Name == tool.BashName:
		return classifyBash(callCommand(call), out)
	case strings.HasPrefix(call.Name, "mcp__"):
		return classified{kind: resultContent, signal: "mcp"}
	}

	return classified{kind: resultContent, signal: call.Name} // SkillUse, ViewImage, the agent tools
}

// classifyBash reads a Bash result: the runner's text ends with "Exit
// code: N" when N is not 0, and "Error: …" when the command did not run.
func classifyBash(command, out string) classified {
	if m := exitCode.FindStringSubmatch(out); m != nil {
		if n, _ := strconv.Atoi(m[1]); n != 0 {
			return classified{kind: resultFailure, signal: "exit " + m[1], command: command}
		}
	}
	switch {
	case strings.Contains(out, "\nError: ") || strings.HasPrefix(out, "Error: "):
		return classified{kind: resultFailure, signal: "did not run", command: command}
	case strings.HasPrefix(out, "Command is still running") || strings.HasPrefix(out, "Still running"):
		return classified{kind: resultContent, signal: "still running"}
	}
	for _, p := range cmdparse.ParseScript(command) {
		switch p.Kind {
		case cmdparse.Read:
			return classified{kind: resultContent, signal: "read"}
		case cmdparse.ListFiles:
			return classified{kind: resultContent, signal: "listing"}
		case cmdparse.Search:
			return classified{kind: resultContent, signal: "search"}
		case cmdparse.Unknown:
		}
	}
	if isDump(command) {
		return classified{kind: resultContent, signal: "dump"}
	}
	if testOrBuild.MatchString(command) {
		return classified{kind: resultConfirm, signal: "test or build passed"}
	}
	if len(out) > shortBytes || strings.Count(out, "\n") >= shortLines {
		return classified{kind: resultContent, signal: fmt.Sprintf("long output (%d bytes)", len(out))}
	}

	return classified{kind: resultConfirm, signal: "short output"}
}

// isDump reports whether any command of the script dumps files or history.
func isDump(command string) bool {
	for _, part := range commandSep.Split(command, -1) {
		part = strings.Join(strings.Fields(part), " ")
		for _, d := range dumps {
			if part == d || strings.HasPrefix(part, d+" ") {
				return true
			}
		}
	}

	return false
}

// callCommand is a call's command as normalized: Bash's command without
// its wrappers and with single spaces, else the tool's name and input.
func callCommand(call llm.ToolCall) string {
	if call.Name == tool.BashName {
		var args struct {
			Command string `json:"command"`
		}
		if json.Unmarshal([]byte(call.Arguments), &args) == nil {
			return strings.Join(strings.Fields(cmdparse.Strip(args.Command)), " ")
		}
	}

	return call.Name + " " + strings.Join(strings.Fields(call.Arguments), " ")
}
