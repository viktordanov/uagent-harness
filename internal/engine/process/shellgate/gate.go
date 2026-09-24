// Package shellgate applies the command rules to the process engine's
// commands. The runner runs every Bash command as `$SHELL -c <command>`, so
// uah points $SHELL at a small script that runs uah itself as the gate: it
// decides the command with the same approver as the embedded engine, with
// no one to ask, then execs the sandboxing shell, the plain shell (an allow
// rule), or nothing (a forbidden or prompt rule).
package shellgate

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/viktordanov/uagent-harness/internal/approval"
	"github.com/viktordanov/uagent-harness/internal/rules"
	"github.com/viktordanov/uagent-harness/internal/sandbox"
)

// Command is the hidden first argument that makes uah the gate.
const Command = "__shell-gate"

// Config is what the gate needs; the script carries it as JSON.
type Config struct {
	Rules  []rules.Rule    `json:"rules"`
	Policy approval.Policy `json:"policy,omitempty"`
	// Sandboxed runs a command in the permission mode's sandbox.
	Sandboxed string `json:"sandboxed"`
	// Unsandboxed runs a command outside it, for an allow rule.
	Unsandboxed string `json:"unsandboxed"`
}

// Write writes the gate script for c into dir and returns its path: a
// shell whose `-c <command>` runs `exe __shell-gate <config> -c <command>`.
func Write(dir, exe string, c Config) (string, error) {
	data, err := json.Marshal(c)
	if err != nil {
		return "", fmt.Errorf("failed to encode the shell gate: %w", err)
	}
	script := "#!/bin/sh\n# Written by uah: applies the command rules, then runs the command.\nexec " +
		sandbox.Quote(exe) + " " + Command + " " + sandbox.Quote(string(data)) + " \"$@\"\n"

	return sandbox.WriteScript(dir, script) //nolint:wrapcheck // its errors name the file
}

// Decide is the command line that runs the shell's arguments, or the reason
// they do not run. Only `-c <command>` is decided, as the runner calls the
// shell; any other call (a shell started by a command) runs in the sandbox.
func (c Config) Decide(ctx context.Context, args []string, cwd string) (argv []string, refused string) {
	if len(args) < 2 || args[0] != "-c" {
		return append([]string{c.Sandboxed}, args...), ""
	}
	d := approval.New(approval.Config{Policy: c.Policy, Rules: c.Rules}).
		Decide(ctx, approval.Request{Command: args[1], Cwd: cwd}, nil)
	switch d.Run {
	case approval.Unsandboxed:
		return append([]string{c.Unsandboxed}, args...), ""
	case approval.Sandboxed:
		return append([]string{c.Sandboxed}, args...), ""
	case approval.Deny:
	}

	return nil, d.Reason
}
