package instructions

import _ "embed" // CodexPrompt

// CodexPrompt is Codex's base instructions for gpt-6-sol, verbatim: the
// model's model_messages.instructions_template in
// codex-rs/models-manager/models.json at rust-v0.156.1 (Apache-2.0,
// Copyright 2025 OpenAI). Codex sends each catalog model its own template;
// gpt-6-sol is uah's default model on openai-codex. `uah prompts init` writes it as
// system-codex.md, which model_instructions_file can name instead of
// system.md. It names Codex's tools, not all of which uah has.
//
//go:embed codex_prompt.md
var CodexPrompt string
