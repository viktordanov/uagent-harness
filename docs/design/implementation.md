# Implementation spec

Status: draft, 2026-09-24. It turns [harness.md](harness.md) and [tui.md](tui.md) into concrete packages, files, types, and milestones.

1. [Repositories and responsibilities](#1-repositories-and-responsibilities)
2. [Decisions made here](#2-decisions-made-here)
3. [Dependency graph](#3-dependency-graph)
4. [Changes in uagent (v0.3.0)](#4-changes-in-uagent-v030)
5. [uagent-harness layout](#5-uagent-harness-layout)
6. [Session package](#6-session-package)
7. [Engines](#7-engines)
8. [Instructions and configuration](#8-instructions-and-configuration)
9. [Hooks](#9-hooks)
10. [TUI packages](#10-tui-packages)
11. [Concurrency and ordering rules](#11-concurrency-and-ordering-rules)
12. [Testing](#12-testing)
13. [Milestones](#13-milestones)
14. [Risks](#14-risks)

References: `UA/` is `github.com/viktordanov/uagent` (local `~/Projects/Code/go-unreal-agent`), `RN/` is `github.com/unreallabsai/unreal-agent@v0.1.1`.

## 1. Repositories and responsibilities

| Repository | Module | Role |
| --- | --- | --- |
| `go-unreal-agent` (public) | `github.com/viktordanov/uagent` | The thin wrapper: one run of the runner with guards. Owns the **event model** (`core`), the **process engine** (`harness`), the **stream** encoding, and run records. Stays small and close to the runner. |
| `go-unreal-harness` (private, this repository) | `github.com/viktordanov/uagent-harness` | The general-purpose harness built on top: sessions, the embedded engine, instructions, configuration, hooks, and the TUI. |

The rule for deciding where code goes: if a one-shot `uagent "<prompt>"` run needs it, or both engines must agree on it (events, run records, the session lock), it belongs in uagent. Everything about long-lived sessions and interaction belongs here.

## 2. Decisions made here

| # | Decision | Choice | Alternative |
| --- | --- | --- | --- |
| I1 | Split | Two repositories as above; this repository depends on uagent | One repository with the harness under `internal/` in uagent |
| I2 | Binary | `uah` (`cmd/uah`): `uah` opens the TUI, `uah run` runs a headless session, `uah sessions` lists sessions | Name it `uagent-harness`, or add a `tui` subcommand to uagent |
| I3 | Package visibility | Everything under `internal/` until a second consumer exists | Public packages from the start |
| I4 | One event model | The embedded engine encodes each runner session item to JSON and feeds it to uagent's `harness.Decoder`, so both engines produce identical `core.Event` values | A second decoder that reads `sessionstore.Item` structs directly |
| I5 | Session lock | `flock` on `<state>/sessions/<id>.lock`, implemented once in uagent and used by both engines and the uagent CLI | A lock in this repository only (the CLI could still corrupt a session the TUI holds) |
| I6 | Configuration format | TOML with `github.com/BurntSushi/toml` v1.6.0 | YAML |

## 3. Dependency graph

```text
cmd/uah ──► internal/tui/bubble ──► internal/tui/render ──► internal/tui/state
   │               │                                             │
   │               └──────────────► internal/session ◄───────────┘ (types only)
   │                                     │
   ├──► internal/config                  ├──► internal/engine (interface)
   ├──► internal/instructions            │        ├──► internal/engine/process ──► UA/harness (process engine)
   └──► internal/hooks                   │        └──► internal/engine/embedded ──► RN/harness/*, UA/harness (Decoder, lock, state)
                                         └──► UA/core, UA/harness (history, lock)
```

External modules: `github.com/viktordanov/uagent` v0.3.0, `github.com/unreallabsai/unreal-agent` v0.1.1 (pinned), `charm.land/bubbletea/v2` v2.0.9, `charm.land/lipgloss/v2` v2.0.6, `charm.land/bubbles/v2` v2.2.1, `charm.land/glamour/v2` v2.0.1, `github.com/BurntSushi/toml` v1.6.0, `github.com/urfave/cli/v3`, `github.com/google/uuid`, `github.com/stretchr/testify`.
Go 1.27 (the runner needs `encoding/json/v2`).

## 4. Changes in uagent (v0.3.0)

Each change is additive; the stream stays at schema version 1. Every item updates `stream/README.md`, the golden files, and the README where it describes behavior.

### U1. User and control inputs in events

| File | Change |
| --- | --- |
| `UA/core/event.go` | Add `UserMessage{At, ID, Text}` and `ControlInput{At, ID, Mode, Effort, Reason}`. |
| `UA/harness/wire.go` | Add `inputDTO{ID, Kind, Payload json.RawMessage}` and `controlDTO{Mode, Reason, Parameters{ReasoningEffort}}`. |
| `UA/harness/decode.go` | Decode `Kind:"input"`: `external` becomes `UserMessage` (payload is a JSON string), `control` becomes `ControlInput`. |
| `UA/stream/serialization.go` | Add `user_message` and `control_input` DTOs. |
| `UA/core/stats.go` | Count `UserMessages` in `Stats`. |
| `UA/testing/fixtures/golden/*` | Regenerate; the fixtures already contain input items. |

### U2. Richer tool and turn events

| File | Change |
| --- | --- |
| `UA/core/event.go` | `TurnStarted` and `ModelResponded` gain `TurnID`; `ToolCalled` gains `Arguments`; `ToolStarted` and `ToolFinished` gain `OpType`, `OutPath`, `ErrPath`; `AssistantMessage` and `ReasoningSummary` gain `Turn`. |
| `UA/harness/wire.go` | Read `TurnID`, and `OutPath` and `ErrPath` from the shell state. |
| `UA/harness/decode.go` | Fill the new fields. |
| `UA/stream/serialization.go` | Add the fields (`turn_id`, `arguments`, `op_type`, `out_path`, `err_path`). |

### U3. Messages with IDs in requests

| File | Change |
| --- | --- |
| `UA/core/run.go` | Add `UserInput{ID, Text}` and `Request.Messages []UserInput`. `Prompt` stays as a shorthand for one message. |
| `UA/harness/wire.go` | `requestDTO` sends `messages[]` with `message_id` when `Messages` is set. |
| `UA/harness/harness.go` | Validate that exactly one of `Prompt` and `Messages` is set; generate missing IDs with `uuid.NewString()`. |

### U4. A run handle and a clean interrupt

| File | Change |
| --- | --- |
| `UA/harness/harness.go` | Add `Start(ctx, req, sink) (*Run, error)`; `Run` becomes `Start` plus `Wait`. |
| `UA/harness/run.go` (new) | `type Run struct` with `Wait() (core.Result, error)`, `Interrupt() error` (SIGINT to the runner, then the teardown after `KillGrace`), `Kill() error` (today's teardown), `SessionID()`, `RunID()`. |
| `UA/harness/process.go` | Separate "interrupt" (SIGINT first) from "kill" (SIGTERM, then SIGKILL); both still kill orphaned tool groups. An interrupted run classifies as `interrupted`. |

### U5. Session lock

| File | Change |
| --- | --- |
| `UA/harness/lock.go` (new) | `LockSession(stateDir, sessionID string) (unlock func() error, err error)` using `syscall.Flock(LOCK_EX\|LOCK_NB)` on `<state>/sessions/<id>.lock`; `ErrSessionBusy` when held. |
| `UA/harness/harness.go` | `Start` takes the lock for the life of the run; a busy session is a preflight-style error (exit code 2 in the CLI). |

### U6. Run records for history

| File | Change |
| --- | --- |
| `UA/harness/state.go` | `Runs() ([]RunRecord, error)` with `RunRecord{Dir, Request core.Request, Result *core.Result, Complete bool}`, including runs without a summary. `LoadRequest(runDir)`, `LoadEvents(runDir, sink)`. `History()` stays for completed runs. |
| `UA/harness/wire.go` | Decode `request.json` back into `core.Request` (prompt, messages, model, effort, session ID). |

### U7. Tests and release

- `UA/harness/harness_test.go`: lock contention (a second `Start` on the same session fails with `ErrSessionBusy`), SIGINT interrupt classification, `messages[]` on stdin with IDs, history including an incomplete run.
- `UA/testing/fakerunner`: echo `messages[]` IDs back as `input` items so delivery acknowledgement can be tested.
- Tag `v0.3.0`.

## 5. uagent-harness layout

```text
go-unreal-harness/
├── README.md
├── go.mod                              module github.com/viktordanov/uagent-harness
├── .golangci.yml                       copied from uagent
├── .github/workflows/ci.yml            build, race tests, lint
├── docs/design/                        harness.md, tui.md, implementation.md
├── bench/tui/                          framework benchmark (separate module)
├── cmd/uah/
│   ├── main.go                         urfave/cli app, exit codes, signal handling
│   ├── tui.go                          default action: open the TUI
│   ├── run.go                          `uah run`: headless session, prints events like `uagent`
│   └── sessions.go                     `uah sessions`, `uah sessions show <id>`
├── internal/session/
│   ├── session.go                      Session, Open, Submit, InterruptAndSend, Interrupt, SetSettings, Close
│   ├── state.go                        the session state machine
│   ├── queue.go                        pending inputs and delivery tracking
│   ├── events.go                       session events
│   ├── settings.go                     Settings and validation
│   └── history.go                      Sessions() and Load() over uagent run records
├── internal/engine/
│   ├── engine.go                       Engine, Run, Capabilities
│   ├── process/process.go              Engine backed by uagent's harness.Start
│   └── embedded/
│       ├── engine.go                   Engine backed by the runner's packages
│       ├── wiring.go                   store, session, operations, inbox, coordinator
│       ├── providers.go                LLM client selection and credentials
│       ├── tools.go                    registry: Bash, ViewImage, skills
│       ├── prompt.go                   default host prompt plus instructions
│       ├── adapter.go                  llm.Adapter wrapper: per-request model and tier
│       ├── tier.go                     Responses clients with service_tier extensions
│       ├── observer.go                 session items to core events through uagent's Decoder
│       ├── inputs.go                   inbox submissions: external, settings, hard, when_idle
│       └── guards.go                   timeout, disk limit, environment
├── internal/instructions/
│   ├── discover.go                     find instruction files
│   └── assemble.go                     build the host prompt with a size cap
├── internal/config/
│   ├── config.go                       Config types and merge order
│   └── load.go                         read user and project TOML
├── internal/hooks/
│   ├── hooks.go                        Runner: Fire(event) -> Decision
│   ├── payload.go                      input and output JSON types
│   ├── exec.go                         run a command with timeout and stdin
│   └── trust.go                        hash-based trust store
├── internal/tui/
│   ├── state/                          pure reducer (see section 10)
│   ├── render/                         lipgloss rendering with a line cache
│   └── bubble/                         the Bubble Tea shell
└── testing/
    ├── fixtures/                       captured session runs and scripted model responses
    ├── fakellm/                        scripted llm.Adapter for the embedded engine
    └── harnesstest/                    helpers: temp state dirs, credentials, fake runner build
```

## 6. Session package

### Types

```go
// settings.go
type Settings struct {
    Provider, Model, Effort, ServiceTier string // ServiceTier: "" or "priority"
    Workspace                            string
    Timeout                              time.Duration
    MaxDisk                              int64
}
func (s Settings) Validate() error // effort allowlist, model syntax, tier values

// events.go (all implement core.Event)
type SessionOpened struct{ At time.Time; ID string; Resumed bool; Engine string }
type InputQueued struct{ At time.Time; Input core.UserInput }
type InputDelivered struct{ At time.Time; ID string }        // from the runner's UserMessage echo
type SettingsChanged struct{ At time.Time; Settings Settings; Applied Applied } // AppliedLive, AppliedNextRun
type Idle struct{ At time.Time }
type InstructionsLoaded struct{ At time.Time; Files []string; Bytes int; Truncated bool }

// session.go
type Session struct { /* engine, state machine, queue, events channel */ }
func Open(ctx context.Context, eng engine.Engine, opts Options) (*Session, error)
func (s *Session) Submit(text string) (core.UserInput, error)   // Enter
func (s *Session) SteerNow(text string) (core.UserInput, error) // Ctrl+Enter
func (s *Session) Interrupt() error
func (s *Session) SetSettings(Settings) (Applied, error)
func (s *Session) Events() <-chan core.Event
func (s *Session) Close() error

// history.go
type Info struct {
    ID, FirstPrompt, Model, Provider, Workspace string
    Runs                                       int
    LastActivity                               time.Time
    Status                                     core.Status
    Tokens                                     core.Tokens
}
func Sessions(stateDir string) ([]Info, error)                  // reads request.json and summary.json only
func Load(stateDir, id string) ([]core.Event, []core.Result, error) // concatenates runs in start order
```

### State machine (`state.go`)

| State | Enter (`Submit`) | Ctrl+Enter (`SteerNow`) | Run ends | `Interrupt` |
| --- | --- | --- | --- | --- |
| `Idle` | Start a run with the message | Same as Enter | — | No-op |
| `Running` | Queue | Process engine: interrupt, then start a run with the queue plus the message. Embedded engine: submit to the inbox now | Queue non-empty: start a run with the whole queue. Empty: `Idle` | Interrupt, then `Idle` (the queue stays) |
| `Interrupting` | Queue | Queue | Same as `Running` | No-op |
| `Closed` | Error | Error | — | — |

The queue delivers all pending messages in one request (`Request.Messages`), in order, and marks each `InputDelivered` when the runner echoes its ID.
On the embedded engine, "run ends" means the coordinator reached idle, and queued messages go to the inbox without starting a new process.

## 7. Engines

### Interface (`internal/engine/engine.go`)

```go
type Capabilities struct {
    LiveInput     bool // messages reach a running agent
    LiveEffort    bool
    LiveModel     bool
    ServiceTier   bool
}

type Engine interface {
    Name() string
    Capabilities() Capabilities
    // Start runs until the agent is idle (process engine: until exit).
    Start(ctx context.Context, req core.Request, settings session.Settings, sink core.Sink) (Run, error)
}

type Run interface {
    Send(input core.UserInput) error          // ErrUnsupported without LiveInput
    SetEffort(effort string) error            // ErrUnsupported without LiveEffort
    SetModel(model string) error              // ErrUnsupported without LiveModel
    Interrupt() error
    Wait() (core.Result, error)
}
```

### Process engine (`internal/engine/process/process.go`)

A thin adapter over uagent: `Start` calls `UA/harness.Start` with `Request.Messages`, `Model`, `Effort`, and returns a `Run` whose `Send`, `SetEffort`, and `SetModel` return `ErrUnsupported`. The session queue covers them.

### Embedded engine (`internal/engine/embedded/`)

`Start` reproduces `RN/cmd/internal/agentrunner/run.go:144-447` with uagent's guards:

1. **Guards** (`guards.go`): call `UA/harness.Preflight`, take `UA/harness.LockSession`, and never load the workspace `.env`. Run the timeout and disk watchdog the same way the process engine does, sharing `UA/harness` helpers where possible.
2. **Store and session** (`wiring.go`): `localfile.New(<state>/sessions)`, then `store.Resume(ctx, id)` or `store.Create(ctx, id)` on `fs.ErrNotExist` (the logic of `run.go:647-668`).
3. **LLM client** (`providers.go`, `tier.go`): the same provider table as the runner (`openai`, `openai-codex`, `openrouter`, `fireworks`, `ollama`), built from `RN/harness/llm/clients/*`. For `openai` and `openai-codex` with `ServiceTier` set, build a `responsesapi` adapter with `Config.Extensions{"service_tier": "priority"}` instead of the bundled client.
4. **Adapter wrapper** (`adapter.go`): an `llm.Adapter` that sets `request.Model.ID` from the current settings and routes to the standard or priority client, so `/model` and `/fast` apply from the next request.
5. **Tools** (`tools.go`): `bash.New` with the user's `$SHELL` in the workspace and operations under `<state>/sessions/operations/<id>`, `viewimage.New`, and skills from `<workspace>/.harness/skills` through `tool.NewRegistry` and `tool.DiscoverSkills`.
6. **Prompt** (`prompt.go`): the runner's default host prompt, copied verbatim from `run.go:45-50`, followed by the instructions from `internal/instructions`. Set with `builder.SetSystemPrompt`; model and effort with `builder.SetModel`.
7. **Inbox** (`inputs.go`): `inbox.New(ctx, restored.ExternalInputIDs)`. Submit a `settings` control, then the messages with their IDs. Do **not** submit `when_idle` up front: the session submits it only on `Close`, which is what keeps the agent alive for steering.
8. **Events** (`observer.go`): `store.AddObserver` receives each `sessionstore.Item`; encode it with `json.Marshal` and pass the bytes to one `UA/harness.NewDecoder()` per session, then to the sink. Also append the bytes to the run's `events.jsonl`, so run records match the process engine byte for byte. Detect idle when the coordinator has no pending model call or operations, and emit `session.Idle`.
9. **Coordinator**: `coordinator.New(coordinator.Dependencies{...})` and `Run(ctx)` in a goroutine; `Wait` returns when it stops.
10. **Controls**: `Send` submits an `external` input; `SetEffort` submits a `settings` control; `Interrupt` submits `hard`, waits for the coordinator to stop, and opens a new coordinator on the same store for the next input.

`AddObserver` is not safe during concurrent persists (`RN/harness/sessionstore/sessionstore.go:86-87`): register it before the coordinator starts and never change it while it runs.

**As built (M5).** Three things differ from the plan above:

- **uagent's lifecycle.** uagent v0.4.0 gained `harness.Backend`. The embedded engine is a backend, so preflight, the lock, run records, the timeout, the disk limit, orphan cleanup, and statistics are uagent's own code. The observer writes runner JSONL into the harness's pipe, and uagent decodes it and saves it as `events.jsonl`.
- **Stopping when idle.** `when_idle` is submitted up front, as the runner does, so a run ends when the agent is idle. Live messages keep it running because the coordinator counts them as pending input. A message that arrives after the agent decided to stop is requeued by the session for the next run.
- **Fast mode.** `openai` and `openai-codex` build a second `responsesapi` adapter with `service_tier: "priority"` on first use. The Codex credential reader is copied from the runner (MIT) because `openaicodex` keeps it private.

The Codex backend accepted `service_tier: "priority"` in a real probe. Resuming a `gpt-6-sol` session with encrypted reasoning on `gpt-6-luna` also worked.
