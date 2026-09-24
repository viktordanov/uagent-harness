<!-- memoria:section id="overview" files="sandbox.go shell.go" -->
# Sandbox

The sandbox package runs shell commands inside the operating system's sandbox, as Codex does: Seatbelt (`/usr/bin/sandbox-exec`) on macOS and bubblewrap (`bwrap`, which must be installed) on Linux. It decides what a command may write and whether it has network; it does not decide whether a command runs at all, which is [the approver's job](../approval/README.md).

<!-- memoria:export id="summary" -->
Commands run in the operating system's sandbox, as in Codex: Seatbelt on macOS and bubblewrap on Linux. The default mode, workspace-write, lets commands read the whole disk and write only the workspace and temporary directories, without network, and keeps .git, .uagent, .agents, and .codex read-only.
<!-- /memoria:export -->

The profile and the bubblewrap layout are adapted from Codex rust-v0.156.1 (Apache-2.0; see `seatbelt/LICENSE-codex`). The decisions are recorded in the [sandbox plan](../../docs/design/sandbox.md), and the keys are in the [configuration reference](../../docs/configuration.md#sandbox-and-approvals).

1. [Modes](#modes)
2. [How a command is sandboxed](#how-a-command-is-sandboxed)
3. [Platforms](#platforms)
4. [Environment](#environment)
5. [Tests](#tests)
<!-- /memoria:section -->

<!-- memoria:section id="modes" files="sandbox.go gitdir.go" -->
## Modes

The mode comes from `--sandbox`, `UAH_SANDBOX`, or `sandbox_mode`. The names are Codex's.

| Mode | Commands can |
| --- | --- |
| `workspace-write` (default) | Read any file. Write the workspace, `/tmp`, `$TMPDIR`, and `writable_roots`, except the protected paths. No network unless `network_access = true` |
| `read-only` | Read any file; write nothing; no network |
| `danger-full-access` | Anything the user can: no sandbox |

`Policy.Writable` returns the writable roots with symlinks resolved. `Protected` returns the paths that stay read-only inside each root: `.git`, `.uagent`, `.agents`, and `.codex`, and the directory a worktree's `.git` file points to. They are protected because a sandboxed command could otherwise plant code that runs later outside the sandbox, such as a git hook. So `git commit` needs an escalation.
<!-- /memoria:section -->

<!-- memoria:section id="shell" files="shell.go denied.go sandbox.go" -->
## How a command is sandboxed

`Policy.Wrap(argv)` returns the command line that runs `argv` under the policy. `Shell` builds on it: it writes a small script to `<state>/sandbox/sh-<hash>` that execs the sandbox around the real shell, so `<script> -c <command>` runs `<sandbox> <real shell> -c <command>`. Scripts are named by their content (`WriteScript`), so a session reuses one and a changed policy gets a new one.

The engines use the script differently:

| Engine | Use |
| --- | --- |
| embedded | Bash has two translators: one with the sandboxing script as its shell and one with the real shell. The approver picks one per command. When a sandboxed command fails and `Denied` says the output looks like a sandbox denial (Codex's keywords, plus uah's network errors), the model is told it can ask to run the command outside the sandbox |
| process | `SHELL` is the sandboxing script, so the runner sandboxes every command. It cannot ask for escalation. With command rules, `SHELL` is the process engine's gate script instead, which applies the rules and then execs this script, or the one without a sandbox for an `allow` rule ([the process engine](../engine/README.md#the-process-engine)) |

When the platform has no sandbox, `Wrap` returns `ErrUnavailable`. The embedded engine then asks for approval for every command that no rule allows; the process engine runs commands without a sandbox and says so.
<!-- /memoria:section -->

<!-- memoria:section id="platforms" files="seatbelt.go bwrap.go wrap_darwin.go wrap_linux.go wrap_other.go seatbelt/base.sbpl seatbelt/network.sbpl seatbelt/preferences.sbpl" -->
## Platforms

| Platform | File | How |
| --- | --- | --- |
| macOS | `seatbelt.go`, `wrap_darwin.go`, `seatbelt/*.sbpl` | `sandbox-exec -p <profile> -D...`: Codex's base profile, full-disk read, writes under each writable root except its protected paths, and the network rules. Paths go in as `-D` parameters, never into the profile text. Only `/usr/bin/sandbox-exec` is used, never one on `PATH` |
| Linux | `bwrap.go`, `wrap_linux.go` | `bwrap` from `PATH`: the disk read-only, each writable root bound writable with its protected paths bound read-only over it, new user, PID, and IPC namespaces, a new network namespace (no network) unless the policy allows it, and all capabilities dropped. Where a container forbids mounting `/proc`, the layout leaves it out, as Codex does |
| Other | `wrap_other.go` | No sandbox (`ErrUnavailable`) |

On Linux, a protected name that does not exist yet, such as `.git` in a workspace that is not a repository root, is not protected: bubblewrap can only mount over existing paths. Codex creates an empty directory for it, which breaks git in a subdirectory of a repository, so uah does not. Seatbelt protects missing names.
<!-- /memoria:section -->

<!-- memoria:section id="environment" files="env.go shell.go" -->
## Environment

Commands get the whole environment, as in Codex. `EnvPolicy` is Codex's `[shell_environment_policy]`: `inherit` (`all`, `core`, `none`), `ignore_default_excludes = false` to drop `*KEY*`, `*SECRET*`, and `*TOKEN*`, then `exclude`, `set`, and `include_only`, in Codex's order. When the policy is not the default, the script starts with `env -i` and copies each kept variable as `NAME="$NAME"` when the command runs, so no inherited value is written to disk.
<!-- /memoria:section -->

<!-- memoria:section id="tests" files="seatbelt_test.go bwrap_test.go shell_test.go denied_test.go env_test.go wrap_darwin_test.go wrap_linux_test.go" -->
## Tests

| Test | Pins |
| --- | --- |
| `seatbelt_test.go`, `bwrap_test.go` | The profile and the bubblewrap arguments for each mode, against golden files in `testdata` |
| `wrap_darwin_test.go`, `wrap_linux_test.go` | Real sandboxed commands: writes inside and outside the roots, protected paths, network, exit codes, and inherited file descriptors. Each runs only on its platform |
| `shell_test.go`, `env_test.go`, `denied_test.go` | The script, the environment policy, and the denial heuristic |

The package has build-tagged halves, so lint runs for both linux and darwin.
<!-- /memoria:section -->
