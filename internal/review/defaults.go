package review

import "github.com/unreallabsai/unreal-agent/harness/llm"

// Who approves an action that needs approval: approvals_reviewer's values,
// as in Codex.
const (
	ReviewerAuto = "auto_review"
	ReviewerUser = "user"
)

// CodexProvider is the provider whose backend serves CodexModel.
const CodexProvider = "openai-codex"

// CodexModel is Codex's review model on ChatGPT sign-in
// (DEFAULT_APPROVAL_REVIEW_PREFERRED_MODEL in
// codex-rs/model-provider/src/provider.rs).
const CodexModel = "codex-auto-review"

// DefaultEffort is the review effort; Codex asks for low when the model
// supports it.
const DefaultEffort = llm.ReasoningEffortLow

// DefaultModel is the review model for a session: CodexModel on
// openai-codex, else the session's own model.
func DefaultModel(provider, sessionModel string) string {
	if provider == CodexProvider {
		return CodexModel
	}

	return sessionModel
}
