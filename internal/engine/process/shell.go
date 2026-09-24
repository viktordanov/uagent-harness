package process

import (
	"errors"

	"github.com/viktordanov/uagent-harness/internal/approval"
	"github.com/viktordanov/uagent-harness/internal/engine/process/shellgate"
	"github.com/viktordanov/uagent-harness/internal/rules"
	"github.com/viktordanov/uagent-harness/internal/sandbox"
)

// Shells builds the runner's $SHELL for each sandbox mode. The runner runs
// every Bash command as `$SHELL -c <command>`, so this shell is where the
// process engine applies the sandbox and the command rules without changing
// the runner.
type Shells struct {
	// Dir holds the scripts.
	Dir    string
	Policy sandbox.Policy
	Env    sandbox.EnvPolicy
	// Real is the user's shell, which runs the command in the end.
	Real string
	// Gate is the executable that applies Rules before a command runs (uah
	// itself, see internal/engine/process/shellgate); "" applies no rules.
	Gate     string
	Rules    []rules.Rule
	Approval approval.Policy
}

// For returns the shell for a sandbox mode. unavailable means the system
// has no sandbox, so commands run without one.
func (s Shells) For(mode sandbox.Mode) (shell string, unavailable bool, err error) {
	outside := s.Policy
	outside.Mode = sandbox.FullAccess
	unsandboxed, err := sandbox.Shell(s.Dir, outside, s.Env, s.Real)
	if err != nil {
		return "", false, err //nolint:wrapcheck // Shell's errors name the file
	}
	inside := s.Policy
	inside.Mode = mode
	sandboxed, err := sandbox.Shell(s.Dir, inside, s.Env, s.Real)
	switch {
	case errors.Is(err, sandbox.ErrUnavailable):
		unavailable, sandboxed = true, unsandboxed
	case err != nil:
		return "", false, err //nolint:wrapcheck // Shell's errors name the file
	}
	if !s.Gated() {
		return sandboxed, unavailable, nil
	}
	gate, err := shellgate.Write(s.Dir, s.Gate, shellgate.Config{
		Rules: s.Rules, Policy: s.Approval, Sandboxed: sandboxed, Unsandboxed: unsandboxed,
	})

	return gate, unavailable, err //nolint:wrapcheck // Write's errors name the file
}

// Gated reports whether the shells apply command rules: there are rules
// and a gate to apply them.
func (s Shells) Gated() bool { return s.Gate != "" && len(s.Rules) > 0 }
