package approval

import "github.com/viktordanov/uagent-harness/internal/rules"

// Rules are the approver's command rules, with the ones "don't ask again"
// added.
func (a *Approver) Rules() []rules.Rule { return a.policy().Rules() }
