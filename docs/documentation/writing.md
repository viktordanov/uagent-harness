# Writing uagent-harness documentation

Describe uah directly, in a neutral third-person voice: "uah runs…" and "The session queues…".
Address the reader directly only in instructions.

## Start with the reader

uah is the interactive harness on top of uagent: sessions, two engines, instructions, configuration, hooks, and a TUI. Keep that framing visible, and keep uagent's own guards described in uagent.
The root README is written for someone who wants to use uah: install, run, keys, engines, instructions, hooks, configuration, and development.
Design and decision records live in `docs/design`; the root README links to them rather than repeating them.
Add a vertical, numbered list of contents near the top when a README has more than two sections.
Use a table only when the reader is choosing between alternatives or looking up a value, such as a key, an event, or a configuration field.
Facts about unreal-agent-runner or Codex behavior carry the version they were checked against.

## Keep maintenance out of the introduction

Follow `docs/documentation/memoria.md` for review mechanics.
Use section mappings as review hints, never as proof that other prose is unaffected.
Exported summaries are one or two sentences, contain no relative links, and read well out of context.
