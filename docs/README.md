<!-- memoria:section id="overview" files="design/harness.md design/tui.md design/implementation.md design/state.md design/sandbox-research.md design/sandbox.md documentation/architecture.md documentation/memoria.md" -->
# Documentation

<!-- memoria:export id="summary" -->
Design records for the harness, the TUI, state storage, and sandboxing, plus the architecture rules and documentation procedure for uagent-harness.
<!-- /memoria:export -->

Design:

1. [Harness design](design/harness.md): what the runner provides, what the harness adds, the two engines, and the accepted scope.
2. [TUI design](design/tui.md): the framework choice, architecture, screens, keys, and commands.
3. [Implementation spec](design/implementation.md): the packages and files in both repositories, types, milestones, tests, and what was built differently.
4. [State storage](design/state.md): what is stored where, and the plan for a rebuildable SQLite index.
5. [Sandboxing and approvals: research](design/sandbox-research.md): how Codex sandboxes and approves commands, and the options for uah.
6. [Sandboxing and approvals: plan](design/sandbox.md): the decisions (Codex's defaults), how a command runs, the packages, and the phases.

Maintenance:

1. [Architecture](documentation/architecture.md): the package roles and rules every change follows.
2. [Writing guidance](documentation/writing.md): voice, README shape, and what belongs where.
3. [Section IDs](documentation/sections.md): the IDs that map README sections to source files.
4. [Memoria procedure](documentation/memoria.md): how to review and acknowledge documentation after a change.
<!-- /memoria:section -->
