package approval

import "github.com/viktordanov/uagent-harness/internal/rules"

// DecideTyped decides a command the user typed themselves (a `!` command
// in the TUI) when such commands follow the agent's rules: a forbidden
// rule refuses it and an allow rule runs it outside the sandbox, as for
// the agent's commands. Anything else runs in the sandbox without asking,
// because typing the command is the user's approval; so neither a prompt
// rule nor the approval policy asks again.
func (a *Approver) DecideTyped(command string) Decision {
	commands, _ := rules.Split(command)
	rule, matched := a.policy().Check(commands)
	switch {
	case matched && rule.Decision == rules.Forbidden:
		return Decision{Run: Deny, Reason: forbiddenReason(rule)}
	case matched && rule.Decision == rules.Allow:
		return Decision{Run: Unsandboxed}
	}

	return Decision{Run: Sandboxed}
}
