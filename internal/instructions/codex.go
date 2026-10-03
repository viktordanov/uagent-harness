package instructions

import (
	_ "embed" // CodexPrompt and DefaultPrompt
	"strings"
)

// CodexPrompt is Codex's base instructions for gpt-6.1-sol, verbatim: the
// model's model_messages.instructions_template in
// codex-rs/models-manager/models.json at rust-v0.159.1 (Apache-2.0,
// Copyright 2025 OpenAI). Codex sends each catalog model its own template;
// gpt-6.1-sol is uah's default model on openai-codex, and a login still on
// gpt-6-sol gets the same text. `uah prompts init` writes it as
// system-codex.md, which model_instructions_file can name instead of
// system.md. It names Codex's tools, not all of which uah has.
//
//go:embed codex_prompt.md
var CodexPrompt string

// DefaultPrompt is uah's base instructions when model_instructions_file is
// not set: CodexPrompt, modified by uah. default_prompt.diff is the whole
// change (Apache-2.0 section 4(b)): the identity, the tool names (Bash,
// SkillUse), questions asked in the final message because uah has no
// asynchronous question tool, Codex's terminal wording for visuals, and
// the Apps and Plugins sections removed. CodexPrompt stays verbatim, so
// the diff can be applied again when Codex's template changes.
// `uah prompts init` writes it as system.md.
//
//go:embed default_prompt.md
var DefaultPrompt string

// SubagentNote follows a subagent's base instructions: two lines of
// Codex's multi_agent.role.subagent text (gpt-6.1-sol's, models.json at rust-v0.159.1).
// The rest of that text names Codex's v2 agent tools, which uah does not
// offer.
const SubagentNote = "When you provide a response in the final channel, that content is immediately delivered back to your parent agent.\n" +
	"In addition, your final answer may be read by a human, so ensure it is legible."

// questionToolText is what the default prompt says about request_user_input
// (hunk 4 of default_prompt.diff), and withoutQuestionTool what it says
// without the tool (ledger item 63's text).
const (
	questionToolText = "When the `request_user_input` tool is available and the answer is a choice between a few plausible options, ask with the tool instead: " +
		"it shows your questions and their options to the user, waits for the answers, and returns them to you, and you then continue. " +
		"Ask with it at the end of the work that does not depend on the answer, or when you cannot proceed without a decision. " +
		"Never use it for permission requests, and do not ask for files or screenshots with it. " +
		"You can ask multiple questions in a single final message or tool call."
	withoutQuestionTool = "You can ask multiple questions in a single final message."
)

// WithoutQuestionTool is prompt with the default prompt's text on
// request_user_input replaced by item 63's, for a session whose agent is
// never offered the tool ([tools.experimental_request_user_input] enabled
// = false). A prompt without that text is returned as it is.
func WithoutQuestionTool(prompt string) string {
	return strings.Replace(prompt, questionToolText, withoutQuestionTool, 1)
}
