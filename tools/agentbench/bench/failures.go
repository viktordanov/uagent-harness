package bench

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/viktordanov/uah/internal/sessionfile"
)

// Failures counts what the mining of the owner's sessions found goes wrong
// in the environment rather than in the task, over a uah run's main agent
// and subagents, from the session files under its uah-state (the calls'
// arguments, statuses, and their operations' results). See
// tools/agentbench/README.md, "Failures".
type Failures struct {
	// Commands are the Bash calls; Calls every tool call.
	Commands int `json:"commands"`
	Calls    int `json:"calls"`
	// Failed are the calls that failed, ByCause by why (failureRules'
	// names, and the tool errors': patch-context-mismatch,
	// reviewer-denied, approval-refused, tool-error); SubagentFailed those
	// of subagents.
	Failed         int            `json:"failed"`
	ByCause        map[string]int `json:"by_cause,omitempty"`
	SubagentFailed int            `json:"subagent_failed"`
	// Wrapped are commands run through `sh -c`, `bash -c`, or `zsh -c`
	// (the model working around a shell it does not trust), Heredocs those
	// with a here-document.
	Wrapped  int `json:"wrapped"`
	Heredocs int `json:"heredocs"`
	// GoCacheOverrides and TmpdirOverrides are commands that set GOCACHE
	// or TMPDIR (or GOTMPDIR) by hand.
	GoCacheOverrides int `json:"gocache_overrides"`
	TmpdirOverrides  int `json:"tmpdir_overrides"`
	// Truncated are outputs cut to the call's max_output_length;
	// TruncatedRetried those whose next request (same agent) runs the same
	// command again or reads a file it read.
	Truncated        int `json:"truncated"`
	TruncatedRetried int `json:"truncated_retried"`
	// Rereads are reads (cat, sed -n, nl, head, tail) of a file the same
	// agent's previous request read too.
	Rereads int `json:"rereads"`
	// AgentsLookups are commands naming AGENTS.md or CLAUDE.md: searching
	// for or reading instructions that are already loaded.
	AgentsLookups int `json:"agents_lookups"`
	// SkillUses are the skills loaded (SkillUse calls), and IncludeReads
	// the commands naming RTK.md, the file the owner's AGENTS.md includes:
	// the startup ritual, in a request of its own or not.
	SkillUses    int `json:"skill_uses"`
	IncludeReads int `json:"include_reads"`
}

// Tool, operation type, and status names of the session files.
const (
	toolBash     = "Bash"
	opShell      = "shell"
	opCompleted  = "completed"
	opFailed     = "failed"
	opCanceled   = "canceled"
	causeToolErr = "tool-error"
)

// Cause groups of the report.
const (
	CauseFish    = "fish-syntax"
	CauseGoCache = "go-cache"
	CauseTmpdir  = "sandbox-tmpdir"
	CauseNetwork = "network"
)

// sandboxCauses are the causes the report counts as a sandbox denial.
var sandboxCauses = []string{"sandbox-write", "sandbox-git-write", "sandbox-docker-socket", "reviewer-denied", "approval-refused"}

// failureRule names a failed command's cause from its command, its output
// (stderr, then stdout's tail), and its exit code.
type failureRule struct {
	name  string
	match func(cmd, out string, code int) bool
}

func re(s string) *regexp.Regexp { return regexp.MustCompile(s) }

var (
	fishSyntax  = re(`Expected a string|Missing end|Unsupported use of '='|fish: `)
	goCache     = re(`Caches/go-build|GOCACHE|build cache|pkg/mod\S*: (?i:permission denied|operation not permitted)`)
	tmpdirFail  = re(`can't create temp file for here document|mkstemp failed on /var/folders|creating work dir: mkdir /var/folders|/var/folders/\S+: Operation not permitted|TemporaryDirectory|No usable temporary directory|cannot create temp file`)
	gitWrite    = re(`\.git/\S*.*(Operation not permitted|Read-only)`)
	denied      = re(`(?i)operation not permitted|read-only file system|PermissionError|permission denied`)
	network     = re(`Could not resolve host|nodename nor servname|dial tcp|lookup \S+ on|check your internet connection|Network is unreachable|Connection refused|getaddrinfo|Failed to connect|no such host|URLError|Couldn't connect`)
	optionError = re(`illegal option|invalid option|unrecognized option|unknown option|sed: -E: No such file|sed: 1: |usage: (sed|grep|find|date|stat|xargs|readlink|cp|ls|head|tail|nl|timeout)|timeout: command not found`)
	notFound    = re(`command not found|Unknown command|executable file not found`)
	testCmd     = re(`go test|pytest|vitest|jest|npm (run )?test|\btest\b`)
	testFail    = re(`FAIL|failed|AssertionError|Error:`)
	buildCmd    = re(`go (build|vet|run)|tsc|golangci|gofmt|node --check`)
	noFile      = re(`No such file or directory|FileNotFoundError|cannot access|does not exist`)
	searchCmd   = re(`^(rg|grep|git grep|find|fd)\b`)
	scriptError = re(`Traceback|SyntaxError|Error:|panic:`)
	timedOut    = re(`(?i)timed? ?out|deadline|killed`)
	rtkPrefix   = re(`^\s*rtk(\s+proxy)?\s+`)
	shellWrap   = re(`^(?:\S*/)?(?:sh|bash|zsh)\s+(?:-[a-z]+\s+)*-[a-z]*c\s`)
	goCacheSet  = re(`\bGOCACHE=`)
	tmpdirSet   = re(`\b(GO)?TMPDIR=`)
	agentsDoc   = re(`AGENTS\.md|CLAUDE\.md`)
	includeDoc  = re(`RTK\.md`)
	readCmds    = []string{"cat", "nl", "head", "tail", "sed", "bat", "less"}
	cmdSplit    = re(`\|\||&&|[|;\n]`)
)

// failureRules are tried in order; the first match names the cause. They
// port the session mining's classifier (/tmp/uah-agentbench/mine).
var failureRules = []failureRule{
	{CauseFish, func(_, out string, code int) bool {
		return strings.Contains(out, "fish:") && (fishSyntax.MatchString(out) || code == 127)
	}},
	{CauseTmpdir, func(_, out string, _ int) bool { return tmpdirFail.MatchString(out) }},
	// rtk's summary of a go test may keep only the path the cache failed at.
	{CauseGoCache, func(_, out string, _ int) bool { return goCache.MatchString(out) }},
	{"sandbox-git-write", func(_, out string, _ int) bool { return gitWrite.MatchString(out) }},
	{"sandbox-docker-socket", func(_, out string, _ int) bool {
		return strings.Contains(out, "docker.sock") && strings.Contains(out, "permission denied")
	}},
	{"sandbox-write", func(_, out string, _ int) bool { return denied.MatchString(out) }},
	{CauseNetwork, func(_, out string, _ int) bool { return network.MatchString(out) }},
	{"bsd-vs-gnu", func(_, out string, _ int) bool { return optionError.MatchString(out) }},
	{"cmd-not-found", func(_, out string, code int) bool { return code == 127 || notFound.MatchString(out) }},
	{"git-not-repo", func(_, out string, _ int) bool { return strings.Contains(out, "not a git repository") }},
	{"test-fail", func(cmd, out string, _ int) bool { return testCmd.MatchString(cmd) && testFail.MatchString(out) }},
	{"build-fail", func(cmd, _ string, _ int) bool { return buildCmd.MatchString(cmd) }},
	{"file-not-found", func(_, out string, _ int) bool { return noFile.MatchString(out) }},
	{"search-no-match", func(cmd, out string, code int) bool {
		return code == 1 && searchCmd.MatchString(cmd) && strings.TrimSpace(out) == ""
	}},
	{"script-error", func(_, out string, _ int) bool { return scriptError.MatchString(out) }},
}

// ClassifyCommand names why a command failed: cmd is the command as the
// model wrote it, out its stderr and stdout, code its exit code.
func ClassifyCommand(cmd, out string, code int) string {
	cmd = stripRTK(cmd)
	for _, r := range failureRules {
		if r.match(cmd, out, code) {
			return r.name
		}
	}
	if strings.TrimSpace(out) == "" {
		return "empty-exit"
	}

	return "other-nonzero"
}

// classifyToolError names a call's error status.
func classifyToolError(err string) string {
	switch {
	case strings.HasPrefix(err, "apply_patch verification failed"):
		return "patch-context-mismatch"
	case strings.Contains(err, "auto-reviewer denied"):
		return "reviewer-denied"
	case strings.Contains(err, "never asks for approval") || strings.Contains(err, "needs t"):
		return "approval-refused"
	default:
		return causeToolErr
	}
}

func stripRTK(cmd string) string { return rtkPrefix.ReplaceAllString(strings.TrimSpace(cmd), "") }

// operation is the part of an operation snapshot the counts read.
type operation struct {
	ID     string
	Type   string
	Status string
	State  struct {
		Input struct {
			Command string
		}
		Result *struct {
			Out, Err string
			ExitCode *int
		}
		TerminalError json.RawMessage
		OutTruncated  bool
		ErrTruncated  bool
		Plan          struct {
			Type string
		}
	}
}

func (o operation) terminalError() string {
	var s string
	if json.Unmarshal(o.State.TerminalError, &s) == nil {
		return s
	}
	if len(o.State.TerminalError) == 0 || string(o.State.TerminalError) == "null" {
		return ""
	}

	return string(o.State.TerminalError)
}

func terminal(status string) bool {
	return status == opCompleted || status == opFailed || status == opCanceled
}

// agentCall is a tool call of one agent's session.
type agentCall struct {
	name, args, err string
	request         int
	ops             []string
}

func (c agentCall) command() string {
	if c.name != toolBash {
		return ""
	}
	var a struct {
		Command string `json:"command"`
	}
	if json.Unmarshal([]byte(c.args), &a) != nil {
		return ""
	}

	return a.Command
}

// agentSession is one session file read for the counts.
type agentSession struct {
	calls    []*agentCall
	requests int
	ops      map[string]operation
}

func readAgentSession(path string) (agentSession, error) {
	s := agentSession{ops: map[string]operation{}}
	_, page, err := sessionfile.Read(path, sessionfile.BeforeFirst, 0)
	if err != nil {
		return s, err
	}
	byID := map[string]*agentCall{}
	for _, it := range page.Items {
		for _, raw := range it.Operations {
			var op operation
			if json.Unmarshal(raw, &op) != nil {
				continue
			}
			if prev, ok := s.ops[op.ID]; !ok || terminal(op.Status) || !terminal(prev.Status) {
				s.ops[op.ID] = op
			}
		}
		switch it.Kind {
		case sessionfile.KindModelResponse:
			var r sessionfile.ModelResponse
			if it.Decode(&r) != nil {
				continue
			}
			for _, o := range r.Response.Output {
				var tc sessionfile.ToolCall
				if o.Type != sessionfile.OutputToolCall || o.Decode(&tc) != nil {
					continue
				}
				c := &agentCall{name: tc.Name, args: tc.Arguments, request: s.requests}
				byID[tc.CallID] = c
				s.calls = append(s.calls, c)
			}
			s.requests++
		case sessionfile.KindToolCallStatus:
			var st sessionfile.ToolCallStatus
			if it.Decode(&st) != nil {
				continue
			}
			c, ok := byID[st.CallID]
			if !ok {
				continue
			}
			if st.Status.Error != "" {
				c.err = st.Status.Error
			}
			for _, w := range st.Status.WaitingFor {
				if !slices.Contains(c.ops, w) {
					c.ops = append(c.ops, w)
				}
			}
		}
	}

	return s, nil
}

// cause is why the call failed, or "" when it did not.
func (s agentSession) cause(c *agentCall) string {
	if c.err != "" {
		return classifyToolError(c.err)
	}
	for _, id := range c.ops {
		op, ok := s.ops[id]
		if !ok {
			continue
		}
		terr := op.terminalError()
		switch op.Type {
		case opShell:
			code := 0
			if op.State.Result != nil && op.State.Result.ExitCode != nil {
				code = *op.State.Result.ExitCode
			}
			if code == 0 && terr == "" && op.Status != opCanceled {
				continue
			}
			if timedOut.MatchString(terr) {
				return "timeout"
			}
			if op.Status == opCanceled {
				return opCanceled
			}
			out := terr
			if op.State.Result != nil {
				out = op.State.Result.Err + "\n" + tail(op.State.Result.Out, 2500) + "\n" + terr
			}

			return ClassifyCommand(cmpOr(c.command(), op.State.Input.Command), out, code)
		case "remote_job":
			if op.Status != opFailed && terr == "" {
				continue
			}
			if op.State.Plan.Type == "uah.agent" {
				return "agent-failed"
			}

			return causeToolErr
		case "view_image", "skill_use":
			if terr != "" {
				return causeToolErr
			}
		}
	}

	return ""
}

func (s agentSession) truncated(c *agentCall) bool {
	for _, id := range c.ops {
		if op := s.ops[id]; op.Type == opShell && (op.State.OutTruncated || op.State.ErrTruncated) {
			return true
		}
	}

	return false
}

// ReadFiles are the files a command reads with cat, nl, head, tail, bat,
// less, or sed -n, segment by segment.
func ReadFiles(cmd string) []string {
	var files []string
	for _, seg := range cmdSplit.Split(cmd, -1) {
		if strings.ContainsAny(seg, "<>") {
			continue // a redirection writes
		}
		f := strings.Fields(stripRTK(seg))
		if len(f) == 0 || !slices.Contains(readCmds, filepath.Base(f[0])) {
			continue
		}
		sed := filepath.Base(f[0]) == "sed"
		if sed && !slices.Contains(f, "-n") {
			continue // sed that edits, not reads
		}
		script := sed
		for _, a := range f[1:] {
			a = strings.Trim(a, `'"`)
			if a == "" || strings.HasPrefix(a, "-") || strings.Contains(a, "*") {
				continue
			}
			if _, err := strconv.Atoi(a); err == nil {
				continue // head -n 50
			}
			if script {
				script = false // sed -n's script

				continue
			}
			files = append(files, filepath.Clean(a))
		}
	}

	return files
}

// count adds one agent's session to f.
func (f *Failures) count(s agentSession, subagent bool) {
	reads := make([][]string, s.requests+1) // the files each request read
	for _, c := range s.calls {
		if c.request < len(reads) {
			reads[c.request] = append(reads[c.request], ReadFiles(c.command())...)
		}
	}
	for _, c := range s.calls {
		f.Calls++
		if cause := s.cause(c); cause != "" {
			f.Failed++
			f.ByCause[cause]++
			if subagent {
				f.SubagentFailed++
			}
		}
		if c.name == "SkillUse" {
			f.SkillUses++
		}
		cmd := c.command()
		if c.name != toolBash {
			continue
		}
		f.Commands++
		bare := stripRTK(cmd)
		if shellWrap.MatchString(bare) {
			f.Wrapped++
		}
		if strings.Contains(cmd, "<<") {
			f.Heredocs++
		}
		if goCacheSet.MatchString(cmd) {
			f.GoCacheOverrides++
		}
		if tmpdirSet.MatchString(cmd) {
			f.TmpdirOverrides++
		}
		if agentsDoc.MatchString(cmd) {
			f.AgentsLookups++
		}
		if includeDoc.MatchString(cmd) {
			f.IncludeReads++
		}
		mine := ReadFiles(cmd)
		if c.request > 0 && overlaps(mine, reads[c.request-1]) {
			f.Rereads++
		}
		if s.truncated(c) {
			f.Truncated++
			if s.retried(c, bare, mine) {
				f.TruncatedRetried++
			}
		}
	}
}

// retried reports whether the request after c's runs c's command again
// (the same first 25 bytes) or reads a file c read.
func (s agentSession) retried(c *agentCall, bare string, read []string) bool {
	head := bare[:min(len(bare), 25)]
	for _, n := range s.calls {
		if n.request != c.request+1 {
			continue
		}
		next := n.command()
		if (head != "" && strings.Contains(stripRTK(next), head)) || overlaps(read, ReadFiles(next)) {
			return true
		}
	}

	return false
}

func overlaps(a, b []string) bool {
	return slices.ContainsFunc(a, func(x string) bool { return slices.Contains(b, x) })
}

// CountFailures reads every session file of a uah run's state directory:
// the main agent's and its subagents'. mainSession, when not "", names the
// main agent's, so the others count as subagents.
func CountFailures(stateDir, mainSession string) (*Failures, error) {
	files, err := filepath.Glob(filepath.Join(stateDir, "sessions", "*.session.jsonl"))
	if err != nil {
		return nil, err
	}
	f := &Failures{ByCause: map[string]int{}}
	for _, path := range files {
		s, err := readAgentSession(path)
		if err != nil {
			return nil, err
		}
		id := strings.TrimSuffix(filepath.Base(path), ".session.jsonl")
		f.count(s, mainSession != "" && id != mainSession)
	}
	if len(f.ByCause) == 0 {
		f.ByCause = nil
	}

	return f, nil
}

// Cause sums the failures of the causes.
func (f *Failures) Cause(causes ...string) int {
	n := 0
	for _, c := range causes {
		n += f.ByCause[c]
	}

	return n
}

// Sandbox is the failures the sandbox's denials caused, besides the build
// cache's and the temporary directory's.
func (f *Failures) Sandbox() int { return f.Cause(sandboxCauses...) }

// Other is the failures of every other cause: a failing test, a bad
// option, a missing file.
func (f *Failures) Other() int {
	return f.Failed - f.Cause(CauseFish, CauseGoCache, CauseTmpdir, CauseNetwork) - f.Sandbox()
}
