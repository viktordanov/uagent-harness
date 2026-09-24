<!-- memoria:section id="overview" files="review.go defaults.go" -->
# Auto-review

The auto-reviewer is Codex's "auto-review" (guardian): one model call judges an action that needs approval before anyone is asked. It fails closed, and a circuit breaker hands the choice back to the user after repeated denials.

<!-- memoria:export id="summary" -->
Before a user is asked to approve an action, the auto-reviewer judges it with one model call and Codex's review policy: the user's messages are trusted context, and the recent tool calls, without their output, are untrusted. It allows or denies with a reason, denies when the review fails, and leaves the choice to the user after three denials in a row.
<!-- /memoria:export -->

The prompts in `prompts/` are Codex's (rust-v0.156.1, `codex-rs/prompts/templates/guardian`, Apache-2.0), trimmed for a reviewer without tools. `[review] policy_file` replaces the policy (`prompts/policy.md`) with a file's text, as Codex's `[auto_review] policy` replaces it inline (`codex-rs/config/src/config_toml.rs:560-565`); the framing and the output contract stay, because `Parse` depends on them. `uah prompts init` writes the default policy to `<config dir>/prompts/review.md` as a starting point, and `DefaultPolicy` returns it. The package has no engine wiring; `internal/engine/embedded/autoreview.go` puts it in front of the user, as described in [the permission pipeline](../approval/README.md). The research and cost figures are in the [sandbox plan](../../docs/design/sandbox.md#auto-review-as-researched).
<!-- /memoria:section -->

<!-- memoria:section id="review" files="review.go prompt.go verdict.go breaker.go defaults.go prompts/policy.md prompts/policy_template.md prompts/output_contract.md" -->
## How a review runs

1. `Render` builds the user message in Codex's framing: the user's messages (trusted), the latest tool calls without their output (untrusted), and the planned action (tool, command, working directory, sandbox mode, requested permissions, justification).
2. `Limits` keep the input near 5,000 tokens besides the fixed prompt: the first user message and then the newest that fit, the last 10 tool calls, and each field cut in the middle when it is too long.
3. One call through `internal/llmcall`, with Codex's policy (or `Config.Policy`, read from `policy_file`) as the instructions and a per-session prompt cache key. An answer that does not parse is asked again, up to Codex's attempt limit, within the timeout.
4. `Parse` reads strict JSON: `outcome` (allow or deny), `risk_level`, `user_authorization`, and `rationale`. Unknown fields are errors.

| Result | What happens |
| --- | --- |
| allow | The action runs; the TUI shows "auto-approved (risk): reason" |
| deny | The action does not run; the model gets the reason and is told not to retry |
| failed (error, timeout, bad answer) | Denied, with risk high |
| ask_user | The circuit breaker is open: PermissionRequest hooks and the user decide |

The breaker opens after 3 denials in a row or 10 in the last 50 reviews, as in Codex. Unlike Codex, a failed review counts as a denial. `Reset` closes it; the engine calls it at each new user message.

| Setting | Default | Key |
| --- | --- | --- |
| Model | `codex-auto-review` on openai-codex, else the session's model | `[review] model` |
| Effort | low | `[review] effort` |
| Timeout | 90 s | `[review] timeout` |
| Policy | Codex's, `prompts/policy.md` | `[review] policy_file` |
| On or off | on | `approvals_reviewer` (`auto_review` or `user`) |
<!-- /memoria:section -->

<!-- memoria:section id="tests" files="review_test.go prompt_test.go probe_test.go testdata/prompt.golden" -->
## Tests

`review_test.go` pins the outcomes, the fail-closed paths (a bad answer, a timeout), the retry, and the breaker. `prompt_test.go` checks the rendered prompt against `testdata/prompt.golden` and the budget. `probe_test.go` makes one real review and runs only by hand: `go test -tags probe -run TestProbe -v ./internal/review/`.
<!-- /memoria:section -->
