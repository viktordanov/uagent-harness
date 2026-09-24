package app

import (
	"path/filepath"
	"slices"

	"github.com/viktordanov/uagent-harness/internal/approval"
	"github.com/viktordanov/uagent-harness/internal/config"
	"github.com/viktordanov/uagent-harness/internal/rules"
)

// pickApprovals is the --ask flag, the configured approval_policy, or
// on-request, and the configured [approvals] prefixes as rules.
func pickApprovals(in Inputs, cfg config.Config) (approval.Policy, []rules.Rule, error) {
	policy, err := approval.ParsePolicy(first(in.Ask, cfg.ApprovalPolicy))
	if err != nil {
		return "", nil, usage(err)
	}
	allow, err := rules.FromPrefixes(cfg.Approvals.Allow, rules.Allow, "[approvals] allow")
	if err != nil {
		return "", nil, usage(err)
	}
	forbid, err := rules.FromPrefixes(cfg.Approvals.Forbid, rules.Forbidden, "[approvals] forbid")
	if err != nil {
		return "", nil, usage(err)
	}

	return policy, slices.Concat(allow, forbid), nil
}

// newApprover loads the rules files, the user's and, for a trusted
// workspace, the project's, and adds the resolved rules. "Don't ask again"
// writes to the user's default.rules.
func newApprover(r Resolved, cfg config.Config, workspace string) (*approval.Approver, error) {
	dirs := []string{config.RulesDir()}
	if cfg.Projects[workspace].Trusted {
		dirs = append(dirs, config.ProjectRulesDir(workspace))
	}
	loaded, err := rules.LoadDirs(dirs...)
	if err != nil {
		return nil, usage(err)
	}

	return approval.New(approval.Config{
		Policy: r.Approval, Rules: append(loaded, r.Rules...),
		RulesFile: filepath.Join(config.RulesDir(), rules.DefaultFile),
	}), nil
}
