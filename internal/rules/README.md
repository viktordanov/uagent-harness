<!-- memoria:section id="overview" files="rules.go parse.go load.go shell.go" -->
# Command rules

The rules package reads Codex's `.rules` files, Starlark `prefix_rule` calls, and matches them against a command's words. The strictest matching decision wins.

<!-- memoria:export id="summary" -->
Command rules are Codex's Starlark prefix_rule entries in .rules files: a rule whose pattern is a prefix of a command's words allows it outside the sandbox, asks first, or forbids it, and the strictest matching decision wins.
<!-- /memoria:export -->

The [approver](../approval/README.md) applies the rules; this package only parses and matches them. The format follows Codex rust-v0.156.1.
<!-- /memoria:section -->

<!-- memoria:section id="format" files="parse.go rules.go load.go" -->
## The file format

```python
prefix_rule(
    pattern = ["git", ["push", "fetch"]],         # words; a list is alternatives
    decision = "prompt",                           # allow (default), prompt, forbidden
    justification = "Pushing changes the remote",  # shown when asking or forbidding
    match = ["git push origin"],                   # examples, checked when the file loads
    not_match = ["git status"],
)
```

| Decision | Effect |
| --- | --- |
| `allow` | Run outside the sandbox without asking |
| `prompt` | Ask first, through the approval pipeline |
| `forbidden` | Never run; the model gets the justification |

`match` and `not_match` are checked when the file loads, so a wrong rule fails loudly. `host_executable` and `network_rule` are accepted and ignored, so Codex rules files load unchanged. A first word that is an absolute path also matches by its base name.

`LoadDirs` reads every `*.rules` file of each directory in name order: `~/.uah/rules`, then `<workspace>/.uah/rules` for a trusted workspace. `AppendAllow` adds a "don't ask again" rule to `default.rules`. `FromPrefixes` turns the configuration's `[approvals] allow` and `forbid` prefixes into rules.
<!-- /memoria:section -->

<!-- memoria:section id="matching" files="shell.go rules.go" -->
## Matching

1. `Split` parses the command with a shell parser into its simple commands: plain words and quotes joined by `&&`, `||`, `;`, and `|`.
2. A command with redirects, assignments, variables, globs, substitutions, subshells, background jobs, or control flow does not split, and `Check` matches no rule for it. `Policy.Forbids` then checks it against the `forbidden` rules alone. It finds every simple command inside the shell command, and takes each command's words up to the first word that is not plain text. A match is found. A command whose words stop early may match too, when its known words agree with a rule so far. A command that does not parse may match any `forbidden` rule. The approver denies all these cases.
3. `Policy.Check` decides for all simple commands together: `forbidden` or `prompt` wins when any of them matches it, and `allow` needs every one of them allowed.

`rules_test.go` pins parsing, the example checks, splitting, the strictest-wins check, loading, and appending.
<!-- /memoria:section -->
