package embedded

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uah/internal/patch"
	"github.com/viktordanov/uah/internal/sandbox"
)

// The auto-verify experiment: after a patch applies, the patch job runs a
// quick check of the projects it touched and adds the result to the
// patch's output, so the model need not spend a turn on it. A check is a
// compile-type command, never a test suite: `go build ./...` for a Go
// module (`go vet ./...` when a test file changed, so the tests compile
// too), the package's typecheck or build script, or node --check, for
// JavaScript and TypeScript, and py_compile for Python. It runs in the
// shell a Bash command of the run's permission mode would run in, and is
// skipped when that command would need approval.

// Auto-verify's limits: all checks of one patch, and the output kept of
// each.
const (
	verifyTimeout = 60 * time.Second
	verifyTail    = 4 << 10
)

// verifyCheck is one command and the directory it runs in.
type verifyCheck struct {
	dir, command string
}

// verifier runs the checks after a patch.
type verifier struct {
	workspace string
	timeout   time.Duration
	// shell is the shell a command runs in now; false when it would need
	// approval, so nothing runs.
	shell func() (string, bool)
}

// verifier is the run's auto-verify, nil when the experiment is off.
func (w *wiring) verifier(req core.Request) *verifier {
	if !w.e.experiments.autoVerify {
		return nil
	}
	user := strings.TrimSpace(w.getenv("SHELL"))
	if user == "" {
		user = "/bin/sh"
	}
	v := &verifier{workspace: req.Workspace, timeout: verifyTimeout, shell: func() (string, bool) { return user, true }}
	if w.e.cfg.Sandbox != nil {
		v.shell = func() (string, bool) {
			shell, err := sandbox.Shell(w.e.cfg.SandboxDir, w.policy(req, w.mode.get().Sandbox()), w.e.cfg.Env, user)

			return shell, err == nil // no sandbox here: each command would ask
		}
	}

	return v
}

// verify runs the checks for the changed files and returns what to add to
// the patch's output, or "" when there is nothing to check.
func (v *verifier) verify(ctx context.Context, changes []patch.Change) string {
	checks := detectChecks(v.workspace, changes)
	if len(checks) == 0 {
		return ""
	}
	shell, ok := v.shell()
	if !ok {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, v.timeout)
	defer cancel()
	var b strings.Builder
	for _, c := range checks {
		b.WriteString("\n\n")
		b.WriteString(runCheck(ctx, shell, c, v.workspace, v.timeout))
	}
	b.WriteString("\n\nuah ran this check automatically after the patch; do not run it again unless you change more.")

	return b.String()
}

// runCheck runs one check and describes how it ended.
func runCheck(ctx context.Context, shell string, c verifyCheck, workspace string, timeout time.Duration) string {
	var out bytes.Buffer
	cmd := exec.CommandContext(ctx, shell, "-c", c.command)
	cmd.Dir = c.dir
	cmd.Stdout, cmd.Stderr = &out, &out
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = time.Second
	err := cmd.Run()
	var end string
	var exitErr *exec.ExitError
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		end = fmt.Sprintf("timed out after %s", timeout)
	case ctx.Err() != nil:
		end = "canceled"
	case err == nil || errors.As(err, &exitErr):
		end = fmt.Sprintf("exit %d", cmd.ProcessState.ExitCode())
	default:
		end = fmt.Sprintf("could not run: %v", err)
	}
	command := c.command
	if rel, err := filepath.Rel(workspace, c.dir); err == nil && rel != "." {
		command = "(cd " + sandbox.Quote(rel) + " && " + command + ")"
	}
	head := fmt.Sprintf("Automatic check after the edit: %s → %s", command, end)
	text := strings.TrimRight(out.String(), "\n")
	if len(text) > verifyTail {
		text = "…" + text[len(text)-verifyTail:]
	}
	if text == "" {
		return head
	}

	return head + "\n" + text
}

// detectChecks returns the checks for the changed files, one per project,
// in the order the files came.
func detectChecks(workspace string, changes []patch.Change) []verifyCheck {
	var checks []verifyCheck
	add := func(c verifyCheck) {
		if !slices.Contains(checks, c) {
			checks = append(checks, c)
		}
	}
	var py []string
	goVet := map[string]bool{}
	for _, path := range changedPaths(changes) {
		switch ext := filepath.Ext(path); {
		case ext == ".go" || filepath.Base(path) == "go.mod":
			if mod := projectDir(workspace, path, "go.mod"); mod != "" {
				goVet[mod] = goVet[mod] || strings.HasSuffix(path, "_test.go")
				add(verifyCheck{dir: mod})
			}
		case ext == ".py":
			py = append(py, path)
		case slices.Contains(jsExts, ext):
			if c, ok := jsCheck(workspace, path); ok {
				add(c)
			}
		}
	}
	for i, c := range checks {
		if c.command == "" { // a Go module
			checks[i].command = "go build ./..."
			if goVet[c.dir] {
				checks[i].command = "go vet ./..."
			}
		}
	}
	if len(py) > 0 {
		add(verifyCheck{dir: workspace, command: pyCompile + quoteRel(workspace, py)})
	}

	return checks
}

// pyCompile compiles Python files with the bytecode cache outside the
// workspace, so the check leaves no __pycache__ behind.
const pyCompile = `PYTHONPYCACHEPREFIX="${TMPDIR:-/tmp}/uah-pycache" python3 -m py_compile `

// jsExts are the files a JavaScript or TypeScript check covers.
var jsExts = []string{".js", ".jsx", ".mjs", ".cjs", ".ts", ".tsx", ".mts", ".cts", ".vue", ".svelte"}

// jsScripts are the package scripts that check without running tests, the
// cheapest first.
var jsScripts = []string{"typecheck", "type-check", "tsc", "check", "build"}

// jsCheck is the check of the package a file belongs to: its first
// script of jsScripts, else node --check for a JavaScript file.
func jsCheck(workspace, path string) (verifyCheck, bool) {
	dir := projectDir(workspace, path, "package.json")
	if dir != "" {
		data, err := os.ReadFile(filepath.Join(dir, "package.json"))
		var pkg struct {
			Scripts map[string]string `json:"scripts"`
		}
		if err == nil && json.Unmarshal(data, &pkg) == nil {
			for _, s := range jsScripts {
				if _, ok := pkg.Scripts[s]; ok {
					return verifyCheck{dir: dir, command: "npm run --silent " + s}, true
				}
			}
		}
	}
	switch filepath.Ext(path) {
	case ".js", ".mjs", ".cjs":
		return verifyCheck{dir: workspace, command: "node --check " + quoteRel(workspace, []string{path})}, true
	}

	return verifyCheck{}, false
}

// changedPaths are the files a patch wrote, deleted, or moved to, that
// still matter to a build.
func changedPaths(changes []patch.Change) []string {
	var out []string
	for _, c := range changes {
		switch {
		case c.MoveAbs != "":
			out = append(out, c.MoveAbs)
		case c.Op == patch.Delete && filepath.Ext(c.Abs) == ".py":
			// a deleted Python file has nothing to compile
		default:
			out = append(out, c.Abs)
		}
	}

	return out
}

// projectDir is the nearest directory from the file's up to the workspace
// that holds marker, or "".
func projectDir(workspace, path, marker string) string {
	for dir := filepath.Dir(path); within(dir, workspace); dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, marker)); err == nil {
			return dir
		}
		if dir == workspace || filepath.Dir(dir) == dir {
			break
		}
	}

	return ""
}

func within(path, root string) bool {
	rel, err := filepath.Rel(root, path)

	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// quoteRel joins the paths relative to the workspace, quoted for the shell.
func quoteRel(workspace string, paths []string) string {
	words := make([]string, 0, len(paths))
	for _, p := range paths {
		if rel, err := filepath.Rel(workspace, p); err == nil {
			p = rel
		}
		words = append(words, sandbox.Quote(p))
	}

	return strings.Join(words, " ")
}
