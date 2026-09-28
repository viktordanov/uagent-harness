# Sandboxing and approvals: research and options

Status: research, 2026-09-24. Nothing here is decided yet. The document ends with a recommendation and the [decisions for the owner](#8-decisions-for-the-owner).

Evidence comes from these sources:

- **Codex source:** openai/codex at tag `rust-v0.156.1` (commit `b412ff32`). This matches the installed `codex-cli 0.156.1`. Paths that start with `CX/` are relative to `codex-rs/` in that repository.
- **Runner source:** `RN/` is `github.com/unreallabsai/unreal-agent@v0.1.1`. `UA/` is uagent (`~/Projects/Code/go-unreal-agent`).
- **Local experiments:** macOS 27.2, `codex sandbox`, `/usr/bin/sandbox-exec`. None of them called a model.
- **Claude Code docs:** `code.claude.com/docs`.

Items marked **(unverified)** come from reading source or docs only, not from a test. Linux behaviour was not tested on a Linux host.

1. [The problem in uah](#1-the-problem-in-uah)
2. [How Codex sandboxes commands](#2-how-codex-sandboxes-commands)
3. [How Codex approves commands](#3-how-codex-approves-commands)
4. [Claude Code, as a second reference](#4-claude-code-as-a-second-reference)
5. [bubblewrap](#5-bubblewrap)
6. [Licensing](#6-licensing)
7. [Options for uah](#7-options-for-uah)
8. [Decisions for the owner](#8-decisions-for-the-owner)
9. [Recommended path](#9-recommended-path)

## 1. The problem in uah

The runner executes every Bash tool call itself, with no sandbox and no approvals:

- **One process per call.** The Bash translator turns each call into a shell operation (`RN/harness/tool/bash/bash.go`). The operation starts `Path: state.Input.Shell, Arguments: ["-c", command]` (`RN/harness/operation/shell.go:519-530`). The process runs in its own process group (`Setpgid: true`, `RN/harness/primitives/process.go:606-609`).
- **Full environment.** `ProcessStartRequest.Environment` is never set, so `exec.Cmd` inherits the runner's whole environment. uagent passes its own environment to the runner and pins only three `UNREAL_HARNESS_LLM_*` variables (`UA/harness/process.go:298-316`). Every command therefore sees every secret in uah's environment.
- **Shell choice.** The runner picks the shell from `getenv("SHELL")` and falls back to `/bin/sh` (`RN/cmd/internal/agentrunner/run.go:316-319`).

This last point matters for the options below. **Whoever controls `SHELL` for the runner controls what program runs each command.** On the process engine, that is uah. So uah can wrap each command without wrapping the whole runner. See option A2.

## 2. How Codex sandboxes commands

### 2.1 Modes and defaults

Codex now expresses sandboxing as *permission profiles*. The three built-in profiles are `:read-only`, `:workspace` and `:danger-full-access` (`CX/config/...` constants `BUILT_IN_PERMISSION_PROFILE_*`). The legacy `sandbox_mode` values `read-only`, `workspace-write` and `danger-full-access` map onto them.

| Mode | Read | Write | Network |
| --- | --- | --- | --- |
| read-only | Whole disk | Nothing (only `/dev/null` and PTYs) | Off |
| workspace-write | Whole disk | Workspace, `/tmp`, `$TMPDIR`, and extra `writable_roots` | Off, unless `sandbox_workspace_write.network_access = true` or a network proxy profile is set |
| danger-full-access | Everything | Everything | On. There is no sandbox at all, unless a managed network proxy forces one. |

**Defaults.** When `sandbox_mode` is not set, a project with a trust decision gets workspace-write. Any other project gets the platform default, which is read-only (`CX/config/src/config_toml.rs:785-800`, `CX/core/src/config/permissions.rs:51-62`).

**Protected metadata.** Inside every writable root, these paths stay read-only (`PROTECTED_METADATA_PATH_NAMES`, `CX/protocol/src/permissions.rs:36-50`):

- `.git`, including the resolved `gitdir:` target when `.git` is a pointer file;
- `.agents`;
- `.codex`, even when it does not exist yet.

The reason in the source comment is that `.git/hooks` would otherwise let a sandboxed command plant code that runs later outside the sandbox (`CX/protocol/src/protocol.rs:1122-1126`).

The side effect: **`git add` and `git commit` fail in workspace-write**, so they need escalation. This was tested; see 2.2.

**Other policy features:**

- *Deny-read globs.* They are lowered into Seatbelt regexes (`CX/sandboxing/src/seatbelt.rs`, `build_seatbelt_unreadable_glob_policy`) or into bwrap masks.
- *Symlink checks.* A writable root with a symlink component is refused (`normalize_writable_root_for_sandbox`).
- *Rename protection.* The ancestors of protected paths cannot be renamed, so a directory rename cannot move protected files out of their carve-out (`PROTECTED_ANCESTOR_*`).

### 2.2 macOS: Seatbelt

**How it runs.** Codex executes `/usr/bin/sandbox-exec -p <policy> -D<KEY>=<path>... -- <command>`. It only uses the binary at that absolute path, never one found on `PATH` (`CX/sandboxing/src/seatbelt.rs:55-59`). All paths are passed as `-D` parameters, never pasted into the policy text. That avoids quoting bugs.

The policy is assembled from these parts (`create_seatbelt_command_args_with_profile`, `seatbelt.rs`, about lines 884-1110):

1. **`seatbelt_base_policy.sbpl`** (116 lines), derived from Chrome's renderer sandbox:
   - `(deny default)`;
   - `(allow process-exec)`, `(allow process-fork)`, and signals and process info only within the same sandbox;
   - a list of read-only `sysctl` names;
   - `/dev/null` writes, PTYs, POSIX semaphores (Python multiprocessing), and a few `mach-lookup` services.
2. **Read policy.** Either `(allow file-read*)` for the whole disk, or `(allow file-read* (subpath (param "READABLE_ROOT_n")))` per root.
3. **Write policy.** For each writable root: `(allow file-write* (require-all (subpath (param "WRITABLE_ROOT_n")) (require-not (literal|subpath EXCLUDED)) (require-not (regex "^root/.git(/.*)?$")) ...))`. Also `(deny file-write-unlink ...)` on the root itself, so a command cannot replace the root.
4. **Network policy.**
   - Network off: nothing is added, so `(deny default)` blocks every socket.
   - Network on: `(allow network-outbound) (allow network-inbound)` plus `seatbelt_network_policy.sbpl` (TLS and DNS mach services).
   - With a proxy: only `(allow network-outbound (remote ip "localhost:<port>"))` for each proxy port, plus DNS on port 53 when local binding is allowed. If no port can be inferred, network fails closed.
5. **Fixed denies:**
   - `seatbelt_read_only_platform_defaults.sbpl`;
   - `(deny mach-lookup (xpc-service-name-prefix ""))`;
   - a deny for Codex's own app-server socket directory;
   - `(deny system-fcntl (fcntl-command 80 110))`, which stops writes through read-only descriptors.

**/tmp.** In workspace-write, `/private/tmp`, `/private/var/tmp` and `$TMPDIR` are the real host directories, and they are writable (`CX/sandboxing/src/seatbelt_scratch.rs`). They are shared with the host, not private.

**Measured behaviour** (`codex sandbox -P <profile> -C <git repo> -- /bin/sh -c ...`):

| Action | `:read-only` | `:workspace` |
| --- | --- | --- |
| Write in the workspace | Denied (`Operation not permitted`) | OK |
| Write `~/x` | Denied | Denied |
| Write `/tmp/x`, `$TMPDIR/x` | Denied | OK |
| Write `.git/x`, `mkdir .codex` | Denied | Denied |
| `git status` / `git add` | OK / not tested | OK / **denied** (`.git/index.lock`) |
| Read `~/.ssh/known_hosts` | OK | OK (reads are not restricted) |
| `curl https://example.com` | `Could not resolve host` | `Could not resolve host` |
| Write to an fd the parent opened (stdout redirected to `~/file`) | OK | OK |
| Exit code | Passed through (`exit 7` gives 7) | Passed through |
| Environment added | `CODEX_SANDBOX=seatbelt`, `CODEX_SANDBOX_NETWORK_DISABLED=1` | Same |

The fd result matters. Seatbelt checks paths when a file is opened. The runner opens the capture files (`out`, `err`) before it starts the process, so a sandboxed command can still write its output to the state directory.

**Overhead.** Mean of 20 runs of `true` on this Mac:

| Invocation | Time |
| --- | --- |
| `sh -c true` | 3.2 ms |
| `sandbox-exec` with a trivial policy | 8.3 ms |
| `codex sandbox -P :workspace` | 24.9 ms (includes Codex config loading) |

The cost is noise compared with model latency.

**Nesting fails.** `sandbox-exec` inside a Seatbelt sandbox fails with `sandbox_apply: Operation not permitted` (exit 71); this was tested. So:

- if uah wraps the whole runner in Seatbelt, it cannot also sandbox single commands;
- commands that use `sandbox-exec` themselves (for example, running `codex sandbox` inside uah) break.

`sandbox-exec` also still works outside a sandbox on macOS 27.2, but its man page marks it **DEPRECATED**. Codex, Claude Code, Chrome and Bazel all depend on it; Apple offers no drop-in CLI replacement.

### 2.3 Linux: bubblewrap and seccomp

*From the source only; not tested on Linux.*

**Helper binary.** A helper binary, `codex-linux-sandbox`, runs in two stages (`CX/linux-sandbox/src/linux_run_main.rs:160-340`). It is also reachable from the `codex` binary through arg0 dispatch.

1. **Outer stage.** It builds bwrap arguments and executes bwrap. Inside bwrap it runs itself again with a hidden `--apply-seccomp-then-exec` flag.
2. **Inner stage.** It runs inside the namespaces and:
   - checks that no capabilities remain;
   - sets `PR_SET_NO_NEW_PRIVS`;
   - installs a seccomp filter;
   - forks the command and passes its exit status through.

**Finding bwrap** (`CX/linux-sandbox/src/launcher.rs:125-208`):

- It uses the system `bwrap` from `PATH`, skipping any candidate inside the cwd. The system copy must support `--as-pid-1` and `--perms`.
- Otherwise it uses a bundled bwrap, **bubblewrap 0.11.2** vendored under `CX/vendor/bubblewrap` and compiled by `CX/bwrap/build.rs`. Its SHA-256 is checked, and it runs through `/proc/self/fd/N`.
- If neither exists, it fails.

**Arguments for a restricted filesystem** (`CX/linux-sandbox/src/bwrap.rs:321-740`):

```text
bwrap --as-pid-1 --new-session --die-with-parent
  --ro-bind / / --dev /dev                      # full-disk read; or --tmpfs / plus --ro-bind per readable root
  --bind <root> <root>                          # each writable root (cwd, /tmp, $TMPDIR, writable_roots), shallow first
  --ro-bind <root>/.git <root>/.git             # protected subpaths re-applied read-only after each writable bind
  --perms 555 --tmpfs <root>/.codex --remount-ro <root>/.codex   # missing protected dir: empty read-only mount
  --perms 000 --tmpfs <deny-dir> --remount-ro <deny-dir>         # deny-read directory
  [--tmpfs /run/WSL]                            # WSL2 interop sockets masked
  --unshare-user --unshare-pid --unshare-ipc [--unshare-net]
  --proc /proc --chdir <cwd> --cap-drop ALL
  -- codex-linux-sandbox --apply-seccomp-then-exec ... -- <command>
```

**Network blocking** uses two layers:

- `--unshare-net`, which leaves an empty network namespace with loopback only;
- a seccomp filter (crate `seccompiler`, `CX/linux-sandbox/src/landlock.rs:175-300`, x86_64 and aarch64 only).

In restricted mode, seccomp returns `EPERM` for `connect`, `bind`, `listen`, `accept`, `sendto`, `ptrace`, `io_uring_*` and similar calls. It allows `socket` only for `AF_UNIX`.

**Landlock** is legacy and deprecated. It is available only with `features.use_legacy_landlock`, it is rejected for any restricted filesystem policy, and there is no automatic fallback to it.

**Without user namespaces**, Codex prints a startup warning. The check runs `bwrap --unshare-user --unshare-net --ro-bind / / /bin/true` with a 500 ms timeout. There is no fallback, so commands then simply fail.

**WSL.** WSL1 is refused. WSL2 works, with interop masked.

**AppArmor.** No code mentions it. Only Codex's CI turns off Ubuntu's restriction: `kernel.apparmor_restrict_unprivileged_userns=0` in `.github/actions/setup-ci/action.yml:27-33`.

### 2.4 Windows (brief)

`[windows] sandbox` accepts `unelevated`, `elevated` or `mxc`.

| Backend | Filesystem | Network |
| --- | --- | --- |
| Unelevated | Restricted token plus ACL grants on workspace roots (`CX/windows-sandbox-rs`) | Environment-variable tricks only (proxy set to `127.0.0.1:9`, offline flags). This is best effort, not enforced. |
| Elevated | One-time admin setup creates local users `CodexSandboxOffline` and `CodexSandboxOnline`; commands run as those users | Windows Firewall and WFP rules for the offline user |
| MXC | Microsoft's container runner (`CX/mxc-sandbox`) | From the permission profile |

Not relevant to uah unless Windows support becomes a goal.

### 2.5 Network proxy

`CX/network-proxy` is a local HTTP proxy (127.0.0.1:3128) plus a SOCKS5 proxy (127.0.0.1:8081).

**Policy:**

- Domain allow and deny lists: `example.com`, `*.x` for subdomains, `**.x` for the apex and subdomains. Deny wins, and an empty allow list blocks everything.
- An optional `limited` mode allows only GET, HEAD and OPTIONS. HTTPS then needs a MITM CA.
- Blocked requests get a 403 with `x-proxy-error`.

**How commands reach it:**

- The environment gets `HTTP(S)_PROXY`, `ALL_PROXY`, and the npm, pip, yarn and Docker variants.
- macOS: Seatbelt allows outbound traffic only to the proxy's localhost ports.
- Linux: the command runs in `--unshare-net`, and a TCP-to-Unix-socket-to-TCP bridge carries its traffic to the host proxy. Seccomp then blocks new AF_UNIX sockets.

**Configuration:** `[permissions.<profile>.network]` with `enabled`, `mode`, `.domains`, and more (`CX/config/src/permissions_toml.rs:113-340`). It can run on its own as `codex-network-proxy --config file.json`.

A program that ignores proxy variables simply cannot reach the network. The proxy is never bypassed.

### 2.6 Does Codex expose its sandbox as a CLI?

**Yes.** `codex sandbox` is visible in `codex --help` and is not marked experimental:

```sh
codex sandbox -P :workspace -C <dir> [--log-denials] [--allow-unix-socket PATH] -- <cmd> [args...]
codex sandbox -c sandbox_mode=workspace-write -- <cmd>     # legacy form
```

**Behaviour, tested on macOS:**

- `-C` requires `-P`.
- Exit codes are passed through.
- It adds about 20 ms per call.
- `--log-denials` prints the denials Seatbelt logged, after the command exits.

**Behaviour from the source:**

- It reads `~/.codex/config.toml`, including `writable_roots`.
- It rebuilds the environment from `shell_environment_policy`.
- It dispatches to Seatbelt, `codex-linux-sandbox`, or the Windows backend.

**Stability.** There is no documented stability promise. The lower-level `codex-linux-sandbox` flags are hidden and have already changed (`--sandbox-policy` became `--permission-profile <JSON>`). A third party could call `codex sandbox` reasonably, but it would depend on the user's Codex install and config, and on a surface OpenAI may change.

## 3. How Codex approves commands

### 3.1 Approval policies

`AskForApproval` is defined in `CX/protocol/src/protocol.rs:986-1026`.

| Policy | Meaning |
| --- | --- |
| `on-request` (default; `on-failure` is an alias) | Commands run in the sandbox without asking. The model may ask to run a command outside the sandbox (`sandbox_permissions: "require_escalated"` plus a `justification`). The user or the auto-reviewer approves or denies. |
| `never` | Never ask. A command that needs approval is rejected and the failure goes to the model. Used with `--yolo` (no sandbox) or in CI. |
| `granular` | Per-category switches, where `false` means auto-reject: `sandbox_approval`, `rules`, `skill_approval`, `request_permissions`, `mcp_elicitations`. |
| `untrusted` | Internal only, for projects marked untrusted. Everything asks unless a rule allows it. Setting it in config is now an error. |

**CLI flags:**

- `-a on-request|never`;
- `--approve-for-me` (alias `--not-so-yolo`), which means on-request, workspace-write and the auto-reviewer;
- `--yolo`, which means never and no sandbox.

**TUI presets:**

- "Read Only": on-request + read-only;
- "Default": on-request + workspace-write;
- "Full Access": never + no sandbox.

### 3.2 Escalation flow

The orchestrator (`CX/core/src/tools/orchestrator.rs`) handles every shell call in this order:

1. **Evaluate rules and policy.** The result is skip, forbidden, or needs-approval.
2. **Approve.** If approval is needed, hooks run first, then the auto-reviewer or the user.
3. **Run the first attempt.**
   - It runs **outside** the sandbox only if every segment matched an `allow` rule, or if the model asked for `require_escalated` and got approval.
   - Otherwise it runs inside the sandbox.
4. **Classify the result.** A failure counts as a *sandbox denial* by heuristic (`CX/sandboxing/src/denial.rs`): the command ran sandboxed, the exit code is not 0, 2, 126 or 127, and the output contains `operation not permitted`, `permission denied`, `read-only file system`, `seccomp`, `sandbox`, `landlock` or `failed to write file`. On Linux, 128+SIGSYS also counts.
5. **Handle a denial.**
   - Under `on-request` and `never`, **Codex does not retry by itself.** The denial output goes back to the model. The model prompt tells it to run the command again with `require_escalated` and a `justification` (`CX/prompts/templates/permissions/approval_policy/on_request.md:32`).
   - Only under `granular` with `sandbox_approval=true` (or the internal `untrusted`) does Codex itself ask "command failed; retry without sandbox?" and then retry unsandboxed.

**Model-side tool parameters** (`CX/core/src/tools/handlers/shell_spec.rs:232-278`):

- `sandbox_permissions`: `use_default` or `require_escalated`;
- `justification`: the question shown to the approver;
- `prefix_rule`: a suggested reusable rule, for example `["git","pull"]`.

Codex refuses to propose rules for a banned list of prefixes: shells with `-c`, `python -c`, `node -e`, bare `git`, `rm`, `sudo`, `env`, `npm run`, and others (`CX/core/src/exec_policy.rs:57-146`).

**Remembering approvals:**

- *Session cache.* It is keyed on the exact command, cwd and permissions (`CX/core/src/tools/sandboxing.rs:40-117`).
- *"Don't ask again for this prefix".* This appends `prefix_rule(pattern=[...], decision="allow")` to `~/.codex/rules/default.rules` and reloads the policy immediately.

On this machine, that file holds 100 rules, all `allow`. Some are broad, such as `["git","push"]` and `["go","get"]`. Others are single full commands.

**Dangerous commands.** There is no "known safe commands" list any more. A dangerous-command check still exists (`CX/shell-command/src/command_safety/is_dangerous_command.rs`): `rm -f`, and recursive checks through `sudo`, `env` and `bash -c`. A dangerous command always asks (or is forbidden under `never`).

**`shell-escalation` crate.** It uses a patched zsh with an `EXEC_WRAPPER`, and sends every `execve` inside a command to Codex for an allow, escalate or deny decision. This is per-exec, not per-command-string. It is behind the `shell_zsh_fork` feature, which is under development and off by default.

### 3.3 Command rules (`.rules`, Starlark)

Source: `CX/execpolicy/README.md` and `CX/execpolicy/src/parser.rs`.

```python
prefix_rule(
    pattern = ["git", ["push", "fetch"]],        # tokens; a nested list means "any of"
    decision = "prompt",                          # allow (default) | prompt | forbidden
    justification = "Pushing affects the remote", # shown in the prompt or rejection
    match = [["git", "push", "origin"]],          # examples, checked when the file loads
    not_match = ["git status"],
)
network_rule(host = "pypi.org", protocol = "https", decision = "allow")
host_executable(name = "git", paths = ["/usr/bin/git"])
```

**Matching:**

- A rule matches when its pattern is a **token prefix** of the command.
- When several rules match, **the strictest wins**: forbidden > prompt > allow (`CX/execpolicy/src/policy.rs:402-411`).

**Compound commands.** `bash -lc "a && b | c"` is parsed with tree-sitter-bash into segments, and every segment is checked (`CX/shell-command/src/bash.rs:29-119`). The parser accepts only plain words, quotes, and `&& || ; |`.

If the script contains anything else, the whole `bash -lc "..."` is treated as one command. That includes redirects, `$(...)`, subshells, variables and control flow. Such a command then falls to the default policy, which means "run in the sandbox" under on-request.

**Where rules load from** (`CX/core/src/exec_policy.rs:662-716`):

- `<layer>/rules/*.rules` for the system layer, the user layer (`~/.codex`), and the project layer (`<repo>/.codex`, only for trusted projects);
- managed `requirements.toml` rules on top.

`codex execpolicy check --rules f.rules -- cmd...` tests a file.

**Relation to the sandbox.** An `allow` rule does more than skip the prompt. It also runs the command **outside the sandbox**. That is why a broad `allow` such as `git push` is powerful.

### 3.4 Auto-review ("guardian")

**Configuration:**

- `approvals_reviewer = "auto_review"` (the owner's `~/.codex/config.toml` has it);
- optional `[auto_review] policy = "..."`, which adds text to the reviewer prompt.

It replaces the human prompt for escalations, network approvals, MCP calls, patches and permission requests. It works only with `on-request` or `granular` (`CX/ext/guardian-reviewer/src/routing.rs:52-60`).

**Model:**

- The preferred model is `codex-auto-review` (`CX/model-provider/src/provider.rs:124`). This model appears as a hidden entry in the owner's `~/.codex/models_cache.json`.
- With API-key auth, the model is `gpt-5.6-luna`.
- If neither is available, it uses the session's model.
- Reasoning effort is `low` when the model supports it.
- The reviewer runs as a read-only, no-network sub-session. It can run commands itself to inspect the workspace (`settings.rs:31-52`).

**Prompt** (`CX/prompts/templates/guardian/policy_template.md` plus `policy.md`, about 18 KB, or about 4.5K tokens). It judges "whether the action poses a risk of irreversible damage":

- Only user and developer messages and AGENTS.md count as trusted. Tool output is "untrusted evidence".
- It assigns a risk level: low, medium, high or critical.
- It assigns a level of user authorization: unknown, low, medium or high.
- The outcome rule is: low and medium → allow; high → allow only with at least medium user authorization and a narrow scope; critical → deny.
- "Sandbox retry or escalation after an initial sandbox denial is not suspicious by itself."

**Context the reviewer receives** (`CX/core/src/guardian/prompt.rs:81-212`):

- A compact transcript of user and assistant messages and tool calls, without reasoning. Limits: 5K tokens per message, 1K per tool entry, 20K message tokens and 10K tool tokens in total, and at most 40 recent entries.
- The planned action as JSON: command, cwd, sandbox permissions and justification.
- Later reviews in the same session send only the transcript delta.

**Output:** JSON `{"risk_level","user_authorization","outcome":"allow|deny","rationale"}`. Only `outcome` is required.

**Failure handling:**

- Timeout: 90 s, with up to 3 attempts. A timeout is reported to the model as "did not finish… do not assume unsafe".
- A reviewer error **fails closed** (deny).
- Circuit breaker: after 3 consecutive denials, or 10 in the last 50 reviews, the turn is interrupted.
- `/approve` lets the user approve one retry of a denied action.

**Latency and cost:** Codex's source states neither. The worst case is about 35K input tokens for the first review (4.5K prompt + up to 30K transcript), then deltas. That makes it a "second small agent" rather than a cheap classifier call **(estimate)**.

### 3.5 How the TUI shows approvals

`CX/tui/src/bottom_pane/approval_overlay.rs:254-918`.

**Layout:**

- Title: "Would you like to run the following command?"
- `Reason: <justification>` in italics.
- The command, with a `$ ` prefix.
- A list of options, each with a single-key shortcut.

**Options:**

| Option | Key |
| --- | --- |
| Yes, proceed | `y` |
| Yes, and don't ask again for commands that start with `git pull` | `p` (only when a prefix rule is proposed) |
| Yes, and don't ask again for this command in this session | `a` (network and permission requests only, by default) |
| No, and tell Codex what to do differently | `Esc` / `n` |

Network prompts add "Yes, and allow this host in the future" and "No, and block this host in the future". Ctrl+A shows the full details.

## 4. Claude Code, as a second reference

Docs: [permissions](https://code.claude.com/docs/en/permissions.md), [permission modes](https://code.claude.com/docs/en/permission-modes.md), [sandboxing](https://code.claude.com/docs/en/sandboxing.md).

**Modes:**

- `default`: asks for edits, Bash and network;
- `acceptEdits`: auto-approves edits and simple file commands in the workspace;
- `plan`: read-only until a plan is approved;
- `auto`: a classifier approves;
- `dontAsk`: denies anything that would prompt;
- `bypassPermissions`: for containers and VMs only.

**Rules.** The `permissions.allow`, `permissions.ask` and `permissions.deny` lists in `settings.json` (managed, user, project and local layers). Checks run deny first, then ask, then allow. Examples:

- `Bash(npm run test:*)`
- `Read(./.env)`
- `WebFetch(domain:example.com)`

For compound commands (`&&`, `||`, `;`, `|`), every subcommand must match. Wrappers such as `timeout` and `nice` are stripped. `/usr/bin/curl` does not match `Bash(curl *)`.

**Sandbox:**

- It uses Seatbelt on macOS and bubblewrap plus `socat` on Linux and WSL2.
- Writes go to the cwd, added directories and a session temp directory. Reads cover the whole disk minus `denyRead`.
- These paths are always protected: `.claude/`, `.git/hooks`, `.git/config`, shell rc files, IDE directories and `.mcp.json`.
- Network goes through an HTTP and SOCKS proxy with a domain allow list. The first connection to a domain asks the user. On Linux, `socat` bridges the proxy into the namespace.
- Credential files can be denied, and environment variables can be masked. The proxy substitutes the real value on egress.
- With auto-allow, sandboxed commands run without prompts.
- A command that fails in the sandbox can be retried with `dangerouslyDisableSandbox`, which goes through the normal permission flow. `allowUnsandboxedCommands: false` turns that off.

**Auto mode:**

- A separate classifier model reviews shell, network and protected-path actions.
- It sees user messages, tool calls and CLAUDE.md, but **not tool results**. This is meant to resist prompt injection from command output.
- After 3 consecutive blocks, or 20 in total, it falls back to prompting.

**Prompt:**

- "Yes"
- "Yes, and don't ask again for <pattern>" (saved to `.claude/settings.local.json`)
- "No", with Tab to add a note that goes back to the model

**Standalone runtime.** The sandbox is published as [`@anthropic-ai/sandbox-runtime`](https://github.com/anthropics/sandbox-runtime), Apache-2.0, npm version 0.0.77. It ships a CLI binary, `srt` (checked with `npm view`). Using `srt` as a general wrapper is plausible, but its CLI contract was **not verified**.

**Difference from Codex.** Claude Code's default is "ask". Codex's default is "sandbox without asking; ask only to leave the sandbox". Codex's model makes far fewer prompts for the same safety, and is the better fit for "safe by default, auto-approved escalation".

## 5. bubblewrap

**What it is:**

- `bwrap` is a small setuid-free C program (LGPL-2.0-or-later) from the Flatpak project.
- It builds a sandbox from Linux namespaces: mount, user, pid, net, ipc and uts.
- It has no policy of its own. The caller's arguments define everything. Its README says: "the level of protection… is entirely determined by the arguments passed" (`CX/vendor/bubblewrap/README.md`).

**Availability:**

- Linux only. There is no macOS or Windows version; macOS needs Seatbelt instead.
- It is packaged in every major distribution (`apt install bubblewrap`).
- Codex falls back to a vendored build.

**Typical "workspace-write, no network" call** (a sketch modelled on Codex's arguments; not tested here):

```sh
bwrap --new-session --die-with-parent \
  --ro-bind / / --dev /dev --proc /proc \
  --bind "$WS" "$WS" --ro-bind "$WS/.git" "$WS/.git" \
  --bind /tmp /tmp \
  --unshare-user --unshare-pid --unshare-ipc --unshare-net \
  --cap-drop ALL --chdir "$WS" \
  -- /bin/bash -c "$CMD"
```

Two things to note about this call:

- `--unshare-net` alone blocks the network. The seccomp layer Codex adds is defence in depth (it also blocks `ptrace` and AF_VSOCK on WSL2).
- `--ro-bind / /` exposes all readable files, including `~/.ssh` and `~/.codex/auth.json`, unless they are masked with `--tmpfs` or `--ro-bind /dev/null`.

**Requirements and pitfalls:**

| Environment | Situation |
| --- | --- |
| Most desktop distributions | Unprivileged user namespaces are on. It works. |
| **Ubuntu 23.10+ and 24.04** | AppArmor restricts unprivileged user namespaces (`kernel.apparmor_restrict_unprivileged_userns=1`). The distribution's `/usr/bin/bwrap` has an AppArmor profile that allows it, so the **system** bwrap works. A copied or bundled bwrap at another path does not, unless an admin adds a profile or sets the sysctl to 0 (see [Ubuntu's announcement](https://ubuntu.com/blog/ubuntu-23-10-restricted-unprivileged-user-namespaces)). Codex's CI sets the sysctl to 0. **(unverified on a real 24.04 host)** |
| Debian before 11, some hardened kernels | `kernel.unprivileged_userns_clone=0` turns user namespaces off. |
| Docker and other containers | The default seccomp profile blocks `unshare` and `mount`, so bwrap fails with "No permissions to create a new namespace". Fixes: run the container with `--privileged` or a custom seccomp profile, or skip bwrap and treat the container as the sandbox. |
| WSL2 | Works (Codex supports it, masking `/run/WSL` and the WSLg root). |
| WSL1 | Does not work (no namespaces). |
| RHEL / CentOS 7 | `user.max_user_namespaces=0` by default. |

**Overhead.** Creating namespaces and mounts typically costs a few milliseconds per call, which is small compared with a model turn **(not measured here; there was no Linux host)**. A new network namespace is the slowest part.

## 6. Licensing

**Codex.** The license is Apache-2.0 (`LICENSE`, `docs/license.md`). A `NOTICE` file names "OpenAI Codex, Copyright 2025 OpenAI", plus Ratatui (MIT).

**bubblewrap.** The copy vendored in Codex is LGPL-2.0-or-later (`CX/vendor/bubblewrap/COPYING`).

**What reuse would require:**

| Reuse | Requirement |
| --- | --- |
| Call `codex sandbox` or `codex-linux-sandbox` as an external program | None beyond using the installed binary. This is not redistribution. |
| Copy or port the `.sbpl` files, the Seatbelt policy builder logic, or the bwrap argument layout into uah (Go) | Allowed under Apache-2.0 §4, with conditions: keep a copy of the license, keep the copyright notices, **state that the files were changed**, and include the relevant parts of Codex's `NOTICE` in uah's distribution. Short policy fragments are probably not copyrightable on their own, but keep the notice anyway; it costs nothing. |
| Reimplement the `.rules` format (`prefix_rule` syntax and semantics) | A file format and its semantics are not copyrightable as such. Writing a compatible parser from scratch is fine. Copying the Rust parser or tests into Go is a derivative work, so the Apache-2.0 conditions above apply. |
| Copy the reviewer prompt text (`policy_template.md`, `policy.md`) | Same Apache-2.0 conditions. Keep the attribution and state the changes. |
| Ship a bwrap binary with uah | LGPL obligations (the source or an offer of it, and the license text). Simpler: require the system `bwrap`, which is also what works under Ubuntu's AppArmor. |
| Use `@anthropic-ai/sandbox-runtime` | Apache-2.0, with the same conditions. |

Apache-2.0 is compatible with uah's MIT-licensed dependency on the runner. uah itself can stay under any license, as long as the notices for included Apache-2.0 material ship with it.

## 7. Options for uah

Four options were asked for (A to D). A2, a `SHELL` shim, came out of the research. It is the most useful near-term option, so it appears between A and B.

### A. Wrap the whole runner process (process engine, available now)

uah spawns `sandbox-exec -p <policy> -- unreal-agent-runner ...`, or `bwrap ... -- unreal-agent-runner` on Linux. Every tool call inherits the sandbox.

- **Network cannot be turned off.** The runner itself must reach the model API. So the policy either allows all network (and gives no exfiltration protection) or goes through a proxy that allows only the provider's host. In the proxy case, commands get the same single-host access.
- **Extra writable paths.** The runner writes its session and operation files under uah's state directory, so that directory must be writable. A command can therefore tamper with session files and other runs' records.
- **All or nothing.** There is no per-command escalation. A denied `git commit` stays denied until uah restarts the runner with a wider policy. On the process engine, a restart is already how steering works, so a "restart unsandboxed for this step" flow is possible but coarse.
- **Nesting.** Nested `sandbox-exec` fails, so A rules out adding per-command sandboxing inside it later.
- **Effort:** about 1–2 days (policy builder, flag, tests).

### A2. A sandboxing `SHELL` shim (both engines, available now)

The runner runs `$SHELL -c <command>`. uah sets `SHELL` for the runner to a small uah program, for example `uah __shell`, which is the same binary. It receives exactly `-c <command>`. The shim:

1. Evaluates the command against the rules (allow, prompt or forbidden).
2. Builds the sandbox for the session's mode and runs `sandbox-exec -p <policy> -D... -- <real shell> -c <command>` (macOS) or `bwrap ... -- <real shell> -c <command>` (Linux). It also:
   - restores the real `SHELL`;
   - removes secrets from the environment (see 8.6);
   - passes the exit code through.
3. On a prompt, or on a likely sandbox denial, asks uah over a Unix socket in the state directory. uah asks the reviewer or shows the TUI prompt. On approval, the shim runs the command again, unsandboxed or in a wider sandbox.

What this gives:

- **Per-command sandbox on the process engine.** It needs no runner changes, because the runner opens the capture files before it starts the process (tested: inherited fds work in Seatbelt).
- **Network off for commands, on for the runner.** This is the key advantage over A.
- **The shim blocks while it waits for approval.** The runner sees a long-running command, which is its normal state for Bash.

What it lacks: the model's intent. The Bash tool schema has no `justification` or `require_escalated` parameters, so uah cannot let the model ask up front. There are two ways around that:

- Escalate after a denial: detect the denial, ask, re-run. This is Codex's `granular` retry path.
- Tell the model in the system prompt to write `# uah: escalate: <reason>` as the first line of a command that needs it. The shim parses that line. This is simple, but it is a convention, not a schema.

Risks:

- *Double execution.* A command that half-succeeded and then hit a denial runs twice when it is re-run. Codex has the same risk.
- *Heuristic denial detection.*
- *Fallback shell.* The shim must be exec'd with a fixed path. The runner falls back to `/bin/sh` only when `SHELL` is empty, so setting it is enough.

**Effort:** about 3–5 days for the macOS profile, Linux bwrap arguments, rules, the socket protocol, and a TUI prompt.

### B. Per-command sandbox, rules and approvals in the embedded engine

The embedded engine builds its own `tool.Registry` (`RN/harness/tool/tool.go:21-47`). uah can register its own Bash translator. It can either wrap `bash.New`, or better, define a Bash tool whose schema adds `justification` and `sandbox` (`"default"` or `"escalate"`), like Codex.

`Translate(ctx, call)`:

1. Parses and checks the rules.
2. Checks the approval policy (the reviewer or the TUI). It can block, or return an error result to the model: "denied: <reason>".
3. Submits a shell operation whose `Shell` is the sandbox launcher: the A2 shim, or `sandbox-exec` with arguments built in.

`TranslateResult` checks for a denial and can attach a hint ("this looks like a sandbox denial; rerun with sandbox=escalate and a justification").

What this gives:

- **Codex's full model.** The model asks up front with a reason, the approver sees the reason, and "don't ask again for this prefix" can be offered.
- **Integration with `PreToolUse` hooks.** They already plan to wrap tool translation (implementation.md §9).

Cost:

- It needs the embedded engine (milestone M5).
- A custom Bash schema changes what the model sees compared with the runner.
- Blocking inside `Translate` while waiting for a human must not stall the coordinator for other calls. This needs checking; running the approval as an operation (`RemoteJobHandler`) keeps it asynchronous.

**Effort:** about 3–4 days on top of A2's shim and policy code, which can be reused as-is.

### C. Delegate to `codex sandbox`

The shim (A2) or the translator (B) runs `codex sandbox -P :workspace -C <ws> -- <shell> -c <cmd>`.

For:

- Codex's tested policies on macOS, Linux and Windows, for free.
- About 20 ms of overhead.
- Exit codes are passed through.

Against:

- It needs Codex installed, and it follows the user's `~/.codex/config.toml`, including `writable_roots`, `shell_environment_policy` and profiles. uah's policy would live in Codex's config.
- There is no stability promise, and its flags have changed across versions.
- On Linux it inherits Codex's bwrap choice and its warnings.

Useful as a **fallback or a reference** ("do we match Codex?") rather than as the core.

**Effort:** about half a day on top of A2.

### D. Containers or VMs

Run the runner, or each command, in Docker, Podman or a Linux VM (on macOS: Docker Desktop, OrbStack, Lima, or Apple's `container` tool).

For:

- The strongest isolation: a separate filesystem and network policy through Docker networks.
- The same setup on every host.
- The runner's `Dockerfile` already exists (`RN/Dockerfile`).

Against:

- Heavy. It needs a daemon or VM and container startup time.
- The toolchain and host credentials must be mapped in.
- The workspace is a bind mount (slow on macOS).
- Wrapping per command in a container costs about 100 ms to 1 s per call.
- The same "the runner needs network" problem as A, if the whole runner runs inside.

It fits as an **opt-in "isolated" mode for untrusted work or CI**, not as the default.

**Effort:** 2–3 days for a basic whole-runner container mode.

### Comparison

| | A: whole runner | A2: SHELL shim | B: embedded translator | C: `codex sandbox` | D: container |
| --- | --- | --- | --- | --- | --- |
| Engine | Process | Both | Embedded | Both (through A2 or B) | Both |
| Available | Now | Now | After M5 | Now (needs Codex) | Now |
| Command network off, model on | No | Yes | Yes | Yes | Only per command |
| Per-command escalation | No (restart) | After denial | Up front with a reason, and after denial | Same as its host (A2 or B) | No |
| macOS / Linux | Yes / Yes | Yes / Yes | Yes / Yes | Yes / Yes (and Windows) | Yes (VM) / Yes |
| Overhead per command | None | ~5–10 ms | ~5–10 ms | ~20–25 ms | 100 ms+ |
| New dependencies | None | None (`bwrap` on Linux) | None | Codex | Docker or a VM |
| Effort | 1–2 d | 3–5 d | +3–4 d | +0.5 d | 2–3 d |

### Auto-approval reviewer

The owner wants escalations approved automatically when they are safe. The reviewer is a separate model call made at the moment of escalation. Two ways to run it:

| | Same provider and subscription | A separate cheap model |
| --- | --- | --- |
| How | A one-off request through the runner's LLM client (embedded engine: import `RN/harness/llm/clients/*`; process engine: uah builds the same client). Model: the session model at `low` effort, or `codex-auto-review` if the Codex backend accepts it from a third-party client **(unverified)** | An API key for a small model (for example `gpt-5.6-luna` as Codex uses with API keys, or a Claude Haiku-class model) |
| Billing | Counts against the ChatGPT or Codex subscription limits; no extra money | Paid per token; needs a key and one more piece of configuration |
| Quality | Good; the same model family as Codex's reviewer | Good enough for a policy check if the prompt is tight |
| Failure modes | Subscription rate limits; the review competes with the main agent for quota | One more credential and outage to handle |

**What the reviewer needs.** This is Codex's context, trimmed:

1. **The action:** the command, cwd, sandbox mode, what the sandbox denied (the last 20 lines of stderr), and the model's justification if there is one.
2. **User intent:** the user's messages in the current session. Limit them, for example to the last 3 messages or 2K tokens. They are the *trusted* part.
3. **Recent agent activity:** the last 5–10 tool calls (command and exit code only, not their output), marked as untrusted. Following Claude Code's rule, **leave out tool output**, because injected text in a file or web page is how a model is tricked.
4. **Policy:** a short fixed prompt of about 1–1.5K tokens. It is modelled on Codex's risk levels and thresholds, plus the user's `[review] policy` text.
5. **Output:** `{"outcome":"allow|deny","risk":"low|medium|high|critical","reason":"..."}`. Fail closed on bad output. A timeout (for example 30 s) falls back to the TUI prompt, or to deny when headless.

**Cost per reviewed command (estimate, not measured):**

| | Input tokens | Output tokens | Latency |
| --- | --- | --- | --- |
| Lean uah reviewer | 3–6K (1.5K policy, 1–2K user messages, 0.5–1K recent calls, 0.3K action). The fixed policy prefix is cacheable | 50–200 at low effort | ~1–4 s |
| Codex's reviewer, for comparison | Up to ~35K on the first review (4.5K prompt, 30K transcript), then deltas | — | — |

A normal agent turn re-sends the whole conversation, typically 20–100K tokens. So a lean review costs about 5–15% of one turn. It happens only on escalation, not on every command. Codex shows that most commands run in the sandbox without any review.

Add Codex's circuit breaker (3 consecutive denials, then stop and ask the user), and log every review decision as an event so the TUI shows it.

## 8. Decisions for the owner

| # | Decision | Recommended default | Alternatives |
| --- | --- | --- | --- |
| S1 | Mechanism | **A2 now, B after M5**, sharing one policy package | A only; C as the core; D as the default |
| S2 | Default mode | **workspace-write**: the workspace, `/tmp`, `$TMPDIR` and uah's scratch space are writable; `.git`, `.uagent`, `.codex`, `.agents` and `.harness` stay read-only. Read-only for untrusted workspaces | read-only everywhere; full access with rules only |
| S3 | Network for commands | **Off by default**; escalation turns it on for that one command. Later, a domain allow-list proxy (Codex's `network-proxy` design) for package registries | On (like Codex with `network_access = true`); proxy from the start |
| S4 | `.git` writes | Keep `.git` read-only (hooks are an escape route). `git add`, `commit`, `fetch` and `pull` escalate, and a default rule set auto-allows them, **unsandboxed only for git itself** | Allow `.git` writes except `.git/hooks` and `.git/config`, as Claude Code does. Fewer prompts, and a small, known risk |
| S5 | Where rules live | `~/.config/uagent/rules/*.rules` (user) and `<ws>/.uagent/rules/*.rules` (only for trusted projects). **Codex's `prefix_rule` syntax** (a Go parser for the small Starlark subset, or `go.starlark.net`). Optionally import `~/.codex/rules/*.rules` read-only | TOML lists in `config.toml`; Claude-style `Bash(git push:*)` strings |
| S6 | What an `allow` rule means | Skip approval **and** run unsandboxed, as in Codex; a rule can instead say `sandbox = "keep"` to skip approval only | Skip approval only |
| S7 | Approval policy names | `on-request` (default), `never` (headless: denials go back to the model), `untrusted` (ask for everything not allowed) | Codex's `granular` flags |
| S8 | Auto-review default | **On for the TUI when the provider is `openai-codex`** (it uses the subscription), with a visible "auto-approved: <reason>" line. Off for `uah run` headless unless configured; headless denials go back to the model | Off everywhere; on everywhere |
| S9 | Reviewer model | The session provider's model at `low` effort (or `codex-auto-review` if accepted); `[review] model` can override | A fixed cheap model with its own key |
| S10 | TUI prompt | Codex's layout: title, `Reason:`, `$ command`, and the options below | Claude Code's "Tab to add a note" |
| S11 | Secrets in the command environment | Remove variables that match `*_KEY`, `*_TOKEN`, `*_SECRET` and `UNREAL_HARNESS_LLM_*` from commands; deny reads of `~/.codex/auth.json`, `~/.ssh`, `~/.aws` and the uagent credential files in the sandbox | Keep the full environment (today) |
| S12 | Linux bwrap source | Require the **system** `bwrap`; if it is missing, or user namespaces are unavailable, warn and fall back to "no sandbox, prompt for every command not allowed by a rule" | Bundle bwrap (LGPL, and fails under Ubuntu's AppArmor) |

The proposed TUI prompt for S10:

```text
╭ Run outside the sandbox? ───────────────────────────────────────────╮
│ Reason: push the release tag (sandbox denied: .git/index.lock)       │
│ $ git push origin v0.3.0                                             │
│ Auto-review: medium risk, user asked to release → needs you          │
│                                                                      │
│ › y  Yes, once                                                       │
│   s  Yes, for this command in this session                           │
│   p  Yes, and always allow commands starting with `git push`         │
│   n  No, and tell the agent what to do instead   (esc)               │
╰──────────────────────────────────────────────────────────────────────╯
```

## 9. Recommended path

**Phase 1: sandbox by default on the process engine (A2). About 1 week.**

- Package `internal/sandbox`:
  - a Seatbelt policy builder (ported from Codex's base policy and write rules, with the Apache-2.0 notice);
  - a bwrap argument builder;
  - `Mode{ReadOnly, WorkspaceWrite, FullAccess}`;
  - protected paths.
- `uah __shell`, the shim. `SHELL` is set for the runner.
- Configuration:

  ```toml
  [sandbox]
  mode = "workspace-write"
  network = false
  writable_roots = []
  ```

  The flag is `--sandbox`. The TUI header shows the mode.
- Denial detection with Codex's heuristic. A denied command returns its normal output, plus one line appended to stderr: `uah: blocked by sandbox (workspace-write)`. The model then knows why.
- Without rules or approvals yet, escalation is manual: `/sandbox full` for the next run.
- Tests:
  - table tests of the generated policies;
  - macOS integration tests that run `sandbox-exec`, like the matrix in 2.2;
  - Linux tests in CI with bwrap (GitHub runners need the AppArmor sysctl, as Codex's CI shows).

**Phase 2: rules and approvals (A2 plus the socket). About 1 week.**

- A `.rules` parser for `prefix_rule` and `network_rule`, with compound-command splitting (`mvdan.cc/sh/v3/syntax` is the Go equivalent of tree-sitter-bash). Anything it cannot parse goes to the default policy.
- A shim-to-uah Unix socket with `Evaluate`, `Ask` and `Record` messages; the TUI approval overlay; "always allow prefix" appends to the user rules file.
- The auto-reviewer, behind `[review] enabled`, with the context in §7, fail-closed behaviour and the circuit breaker. Headless uses `never` semantics unless a reviewer is configured.

**Phase 3: the embedded engine (B), after M5. About 3–4 days.**

- A uah Bash translator with `justification` and `sandbox: "escalate"` in its schema, so the model asks up front with a reason.
- The shim becomes the operation's shell.
- `PreToolUse` hooks and approvals share one decision point.

**Later, or on demand:**

- a domain allow-list proxy (S3);
- a container mode (D) for untrusted repositories;
- `codex sandbox` (C) as an optional backend for Windows.

**Why this order.** A2 gives safe-by-default behaviour to the engine that ships today, keeps the model's network access separate from command network access, and needs no runner change. Everything it builds (policies, rules, the reviewer, the TUI prompt) carries over to B unchanged. B then adds only the part the process engine cannot have: the model stating its reason before it acts.
