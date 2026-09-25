# Third-party notices

uah is licensed under the Apache License, Version 2.0 (see [LICENSE](LICENSE)). Parts of it are adapted from the projects below; each adapted file says so in its header, with the upstream file it came from.

## OpenAI Codex

<https://github.com/openai/codex>, at `rust-v0.156.1`. Copyright 2025 OpenAI. Licensed under the Apache License, Version 2.0; the full text is in [LICENSE](LICENSE) and [internal/sandbox/seatbelt/LICENSE-codex](internal/sandbox/seatbelt/LICENSE-codex), and Codex's NOTICE is in [internal/sandbox/seatbelt/NOTICE-codex](internal/sandbox/seatbelt/NOTICE-codex).

Adapted in uah:

| uah | From Codex |
| --- | --- |
| `internal/patch/parse.go`, `update.go`, `apply.go`, and their tests | `codex-rs/apply-patch` (the patch grammar, parser, context matching, and applier) |
| `internal/patch/tool.go` | The `apply_patch` tool description (`codex-rs/core/gpt_5_1_prompt.md`, `core/assets/tools/apply_patch.lark`) |
| `internal/sandbox/seatbelt/base.sbpl`, `network.sbpl`, `internal/sandbox/seatbelt.go` | `codex-rs/sandboxing` (the Seatbelt profiles and their assembly) |
| `internal/sandbox/bwrap.go` | `codex-rs/linux-sandbox/src/bwrap.rs` |
| `internal/sandbox/env.go` | `codex-rs/protocol/src/shell_environment.rs`, `codex-rs/config/src/shell_environment_policy.rs` |
| `internal/sandbox/denied.go` | `codex-rs/sandboxing/src/denial.rs` |
| `internal/review/review.go` | The auto-review (guardian) prompt, `codex-rs/prompts/templates/guardian` |
| `internal/compaction/compaction.go` | The compaction prompt and summary prefix, `codex-rs/prompts/templates/compact` |
| `internal/agents/prompt.go` | The multi-agent tool descriptions and schemas, `codex-rs/core/src/tools/handlers/multi_agents_spec.rs` |
| `internal/instructions/codex_prompt.md`, `codex.go` | Codex's base instructions for gpt-6-astra, verbatim (`model_messages.instructions_template` in `codex-rs/models-manager/models.json`) |
| `internal/models/bundled.json` | The bundled model catalog, `codex-rs/models-manager/models.json` (a subset of its fields) |

Many other parts follow Codex's behavior (configuration keys, rules, approvals, MCP, subagents); those are uah's own code written against Codex's documented behavior and source, and the design records in [docs/design](docs/design) cite the Codex files they follow.

## unreal-agent

<https://github.com/unreallabsai/unreal-agent>, v0.1.1. Used as a library throughout; adapted in uah:

| uah | From unreal-agent |
| --- | --- |
| `internal/engine/codexauth/codexauth.go` | `harness/llm/clients/openaicodex/credentials.go` (loading the ChatGPT credentials) |
| `internal/engine/embedded/clients.go` | `harness/llm/clients/openai/client.go`, `openrouter/client.go`, `fireworks/client.go`, `ollama/client.go`, and `openaicodex/client.go` (each provider's Responses client: endpoint, headers, prompt cache key placement, request extensions, the codex base URL check and error wrapping), and `harness/primitives/remote.go` (`newRemoteHTTPClient`'s transport settings) |

```
MIT License

Copyright (c) 2026 Unreal Labs

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```
