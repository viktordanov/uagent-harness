# uagent-harness

The general-purpose harness built on [uagent](https://github.com/viktordanov/uagent), the wrapper around unreal-agent-runner.
uagent runs one task with safety guards. This repository adds what long-lived, interactive work needs: sessions with a message queue and steering, an embedded engine for live model, effort, and fast-mode changes, instruction files, configuration, hooks, and a terminal UI.

This repository is in the design stage.

1. [Harness design](docs/design/harness.md): what the runner provides, what the harness adds, the two engines, and the accepted scope.
2. [TUI design](docs/design/tui.md): the framework choice, architecture, screens, keys, and commands.
3. [Implementation spec](docs/design/implementation.md): the packages and files in both repositories, types, milestones, and tests.
4. [TUI framework benchmark](bench/tui/README.md): the measurements behind choosing Bubble Tea v2 (a separate Go module).
