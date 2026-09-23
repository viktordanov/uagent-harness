<!-- memoria:section id="overview" files="bench.sh summarize.py internal/sim/sim.go" -->
# Go TUI options for the uagent live-run UI: survey and benchmark

<!-- memoria:export id="summary" -->
A reproducible survey and benchmark of Go TUI frameworks (Bubble Tea v2, Ultraviolet, vaxis, tcell, tview, gocui) for the uagent live-run UI: cold start, idle cost, streaming CPU, bytes written, and memory.
<!-- /memoria:export -->

Run `./bench.sh` on macOS to rebuild every spike and repeat the measurements, then `python3 summarize.py` to rebuild the tables.
This is a separate Go module, so the root `go test ./...` and lint skip it.
<!-- /memoria:section -->

Date: 2026-09-23. Machine: Apple M4 Max (14 cores), macOS 27.2, Go 1.27.1 darwin/arm64.
No AI models were called. All code is in this directory.

## TL;DR

- **Bubble Tea v2 is good enough, with two conditions.** First, do not use `bubbles/viewport` for the transcript. Second, batch the incoming agent events.
  - The naive idiomatic version is not viable. It calls `viewport.SetContentLines` once per event, and that call is O(total lines) because it re-measures the width of every line. Result: 34–40 s of CPU for 10k events, and the 50k-line run did not finish within 240 s.
  - A windowed transcript that joins only the visible lines, plus 16 ms event batching, fixes this. Streaming 10k events at 200/s then costs 4.2 s CPU, about the same as raw tcell (4.0 s) and Ultraviolet (4.0–5.2 s), and about 2x vaxis (2.2 s).
  - Bubble Tea writes 10x fewer bytes to the terminal than tcell, vaxis or tview (1.2 MB vs 12–16 MB) because its renderer uses scroll-region optimisation.
- **Where Bubble Tea v2 loses measurably:**
  - Cold start: ~24 ms to the first frame vs 3–4 ms for everything else. The first frame waits for the 60 fps render ticker; `WithFPS(120)` brings it to ~15 ms.
  - Idle: ~6 ms of CPU per second from that ticker. The others use 0.
  - Memory: +5–8 MB RSS (≈20 MB vs ≈12 MB for vaxis and tcell).
  - Binary size: +1.7 MB stripped (4.9 MB vs 3.1 MB).
  - None of these is material for an interactive coding-agent UI.
- **vaxis is the most resource-efficient:** lowest CPU, lowest RSS, 3 ms cold start and the richest terminal-feature support. But it has a small community, a thin widget set (no multi-line textarea, no markdown) and no scroll-region optimisation.
- **A "hybrid" with our own widgets** only pays off if raw CPU or RSS is a hard requirement. On Charm's side, that means building on Ultraviolet directly (same renderer as Bubble Tea, no Elm loop). On the non-Charm side, it means vaxis.
- **Recommendation:** Bubble Tea v2, with a virtualised transcript component we own, event batching and a real (non-blinking) cursor. Keep the transcript and layout code independent of `tea.Model`, so we can move to Ultraviolet-direct later if profiling ever demands it.

## 1. Candidate survey

Versions are from `go list -m -versions <module>` and `go list -m -json <module>@latest` on 2026-09-23. Repository activity is from `gh api repos/<owner>/<repo>`.

| Option | Module / latest | Last activity | Stars | Architecture | Notable users |
|---|---|---|---|---|---|
| **Bubble Tea v2** | `charm.land/bubbletea/v2` **v2.0.9** (2026-08-19); `charm.land/lipgloss/v2` v2.0.6; `charm.land/bubbles/v2` v2.2.1; `charm.land/glamour/v2` v2.0.1 | pushed 2026-09-22 | 45.1k | Elm architecture: `Update(msg)` → `View() tea.View`. The View is a *string* plus declarative terminal state (alt screen, mouse mode, cursor, keyboard enhancements, title). A 60 fps (max 120) ticker parses the string into an Ultraviolet cell buffer and diffs it against the previous frame ("cursed renderer": ncurses-style, hard-scroll/scroll-region, ECH/REP, tab optimisations). `View()` is called after **every** message. | **Crush** (Charm's coding agent: bubbletea v2.0.9 + ultraviolet + glamour v2; it ships its own lazy, cached list instead of bubbles/viewport), plus a large ecosystem (glow, soft-serve, gum, …) |
| **Ultraviolet** (Charm renderer, direct API) | `github.com/charmbracelet/ultraviolet` **untagged**, v0.0.0-20260922123528-4e49372c11f9 | pushed 2026-09-22 | 386 | Immediate mode. `Terminal` (raw mode, input decoder, event channel) + `TerminalScreen`/`TerminalRenderer` (cell diff renderer) + `Buffer`/`Window`/`screen.Context` drawing + a Cassowary `layout` package. The README says "API may change". Note: `TerminalScreen` does **not** expose `SetScrollOptim`. You must drive `uv.TerminalRenderer` yourself, as Bubble Tea does, to get the 10x byte savings (see the `-hardscroll` flag in the uv spike). | Bubble Tea v2, Lip Gloss v2, Crush (indirect) |
| **tview** | `github.com/rivo/tview` **v0.42.0** (2025-08-27); master v0.42.1-0.20260811 | pushed 2026-08-11 | 14.1k | Retained widget tree (Flex, TextView, TextArea, List, Table, Form…). Every `Draw` repaints the whole tree into tcell's back buffer, and tcell diffs per cell. **Still on tcell v2** (master too). | **k9s** (via forks derailed/tview + derailed/tcell), many internal tools; `ayn2op/tview` fork used by discordo |
| **tcell v3** | `github.com/gdamore/tcell/v3` **v3.5.0** (2026-09-11). v2 line: v2.13.10 (2026-05-06) | pushed 2026-09-15 | 5.2k | Cell buffer + per-cell diff, immediate mode, no widgets. v3 removed terminfo, `SetContent`→`Put` (graphemes), events via `EventQ()` channel, **removed `SimulationScreen`** (the `vt.MockTerm` replacement is explicitly "not public API"). | **lazygit** (moved gocui in-tree as `pkg/gocui`, on tcell v3.5.0), lf, aretext, gopass, discordo, ov |
| **vaxis** | **`go.rockorager.dev/vaxis`** **v0.17.1** (2026-07-28). The `git.sr.ht/~rockorager/vaxis` and `github.com/rockorager/vaxis` paths also resolve, but their go.mod declares `go.rockorager.dev/vaxis` | pushed 2026-09-18 (GitHub mirror) | 116 (mirror) | Immediate mode on a double cell buffer. It queries the terminal at startup (DA1, DECRQM 2026/2027, kitty kbd, XTGETTCAP, OSC 10/11, explicit width) and **blocks until DA1 answers (3 s timeout)**. Has `widgets/` (textinput, list, pager, **term** = embedded terminal, spinner, …) and the `vxfw` Flutter-like framework (text, richtext, list, textfield, button). Supports primary-screen (inline) mode with `Append` to scrollback. | **aerc** (go.mod: vaxis v0.17.1), senpai, comview |
| **gocui** | `github.com/awesome-gocui/gocui` **v1.1.0** (2022-01-13; master 2026-08-25); `github.com/jroimartin/gocui` v0.5.0 (2021, termbox, dormant); `jesseduffield/gocui` (now superseded by lazygit's in-tree copy) | awesome: 2026-08-25 | 386 / 10.6k | Named views holding ANSI-parsed cell grids. Every `Update` re-runs layout and redraws all views. Built on tcell v2.4-era APIs. | lazygit (own fork) |
| **termdash** | `github.com/mum4k/termdash` v0.22.0 (2026-06-09) | 2026-09-14 | 3.0k | Dashboard widgets (charts, gauges, sparklines) on tcell/termbox. No text editing or transcript widget. **Not spiked**: wrong shape for this app. | dashboards |
| **cview** | `codeberg.org/tslocum/cview` v1.6.4 (2026-04-04) | 2026-04 | – | tview fork with thread-safety changes. Not spiked (same design as tview). | – |
| newer projects | limoni, phoenix-tui, FluffyUI, oat-latte, bubblyui … | 2026 | 10–60 | All under 100 stars. None credible for production yet. | – |

### Terminal feature support (from source, not marketing)

| Feature | Bubble Tea v2 / Ultraviolet | tview (tcell v2.13) | tcell v3.5 | vaxis 0.17 | gocui 1.1 |
|---|---|---|---|---|---|
| Kitty keyboard protocol | yes (`View.KeyboardEnhancements`, progressive flags) | tcell v2.13 enables `CSI >1u` + modifyOtherKeys (seen in the pty dump) | yes (`KeyboardProtocol()`, kitty + win32-input-mode) | yes (full flags by default, key release/repeat) | inherits whatever tcell v2 MVS selects (v2.13.10 here); gocui's own key map is legacy |
| Synchronized output (mode 2026) | yes, after a DECRQM query confirms support | yes (tcell v2.13) | yes | yes, after a DECRQM query | via tcell v2 |
| True colour | yes, with automatic downsampling (colorprofile) | yes | yes | yes (detects RGB via XTGETTCAP/COLORTERM) | yes (OutputTrue) |
| Mouse (SGR) | yes (cell/all motion) | yes | yes | yes (+ SGR pixel mode) | yes |
| Bracketed paste | yes (on by default) | yes | yes | yes | partial |
| Unicode width / graphemes | graphemes (uax29), mode 2027 when available, wcwidth fallback | runewidth + uniseg | grapheme clusters in `Put` | graphemes, mode 2027 + explicit-width protocol | runewidth only |
| Inline (non-alt-screen) mode | yes (+ `tea.Println` to scrollback) | no | no | yes (`PrimaryScreen` + `Append`) | no |
| Images | no (sixel/kitty via raw escapes only) | no | sixel demo | kitty graphics + sixel + fullblock | no |
| Hyperlinks OSC 8 | yes | yes | yes | yes | no |
| Clipboard OSC 52 | yes | yes | yes | yes | no |
| Silent-terminal startup (no reply to queries) | no stall | no stall | **~1 s stall** | **~3 s stall** (DA1 timeout) | no stall |

## 2. Benchmark

### The spike (same behaviour everywhere)

- Full-screen alt screen at 120x40:
  - scrolling transcript viewport (follows the tail; PgUp/PgDn scroll);
  - `─` separator;
  - 3-line input box with `›` prompt and placeholder (Enter submits a "you:" line; the raw spikes insert a newline with Ctrl-J);
  - one status line with coloured background.
- `internal/sim` holds the shared code: flags, the deterministic event generator, the paced producer, counters, a minimal input model for the raw spikes, and the stats file. Each event appends one styled line (dim timestamp, coloured kind tag, text with `▶ ✓ ✗ · —`) and updates the event and token counters in the status bar.
- Producer: a background goroutine emits `-n` events at `-rate`/s (0 = as fast as the UI accepts them), using each framework's native injection path:

  | Framework | Injection path |
  |---|---|
  | Bubble Tea | `Program.Send` |
  | tview | `QueueUpdate` |
  | tcell v3 | own channel in the `select` next to `EventQ()` |
  | vaxis | `PostEventBlocking` (`PostEvent` silently drops when the 1024-slot queue is full) |
  | Ultraviolet | `Terminal.SendEvent` |
  | gocui | mutex-protected slice + `Update` |

- Frame coalescing is the same everywhere: at most 60 frames/s.
  - Bubble Tea has this built in (60 fps ticker).
  - tcell, vaxis and Ultraviolet use a throttle: draw immediately if ≥16.7 ms since the last frame, otherwise arm a one-shot timer.
  - tview and gocui arm a one-shot `time.AfterFunc` per frame.
  - Nothing runs while idle except what the framework itself runs.
- `-exit` quits `-exit-delay` after the frame that shows the final count. The status line ends in `BUSY @@` or `DONE @@`: the harness uses `@@` to detect the first full frame and `DONE` to detect that the final frame reached the terminal.
- Bubble Tea variants:

  | Variant | Description |
  |---|---|
  | `bt` | bubbles/v2 viewport + textarea, `SetContentLines` per event. This is the idiomatic version most examples use. |
  | `btfast` | build tag `fastview`: the same textarea, but the transcript joins only the visible lines. |
  | `-batch` | a forwarder drains all queued events into one `tea.Msg` |
  | `-batch -batchwin 16ms` | collects for up to 16 ms after the first event (≤ one Update/View per frame) |

  All variants use the real terminal cursor (`SetVirtualCursor(false)`), as Crush does, so there are no blink ticks.
- Correctness check: `harness/e2e_test.go` runs every spike binary in a pty and replays the byte stream through a VT emulator (`charmbracelet/x/vt`). It asserts that all seven spikes show the same final status line and last transcript line. It passes. `bin/replay <dump>` prints the reconstructed final screen of any run.

### Harness and exact commands

`harness/main.go` uses `github.com/creack/pty` v1.1.24:
- Starts the binary on a 120x40 pty with `TERM=xterm-256color COLORTERM=truecolor`.
- **Acts as a minimal modern terminal:** it answers DA1, CPR, DECRQM (2026 = supported, others = unknown) and OSC 10/11. Apps that probe the terminal (vaxis, tcell v3, Bubble Tea) behave as they would in Ghostty, kitty or iTerm2. `-respond=false` simulates a terminal that never answers.
- Reads the pty continuously, counts all bytes and timestamps the first byte, `@@` and `DONE`.
- Samples live CPU and RSS with `proc_pid_rusage` (cgo) during idle and after DONE, and collects `wait4` rusage (user+sys CPU, max RSS) at exit.
- Reads a JSON stats file that the app writes at exit: producer start/end, frame count, and heap after GC with the transcript kept reachable.

```sh
./bench.sh build      # go build (default and -ldflags "-s -w") for every spike, plus -tags fastview
./bench.sh sizes      # stat -f %z bin/<spike> bin/<spike>.stripped
./bench.sh cold       # per spike: 1 warm-up, then  bin/harness -bin bin/<s>.stripped -cold -repeat 20
                      #   silent terminal:          bin/harness -bin bin/<s>.stripped -cold -respond=false -repeat 3
./bench.sh idle       # bin/harness -bin bin/<s>.stripped -idle 5s          (CPU measured 1 s..6 s after first frame)
./bench.sh streams    # rate200 : -n 10000 -rate 200 -exit -exit-delay 1s -delay 300ms          (1 run)
                      # burst10k: -n 10000 -exit -exit-delay 1s -delay 300ms                    (3 runs, median)
                      # fps0    : same burst with -fps 0 (draw after every event; raw spikes only)
                      # lines50k: -n 50000 -exit -exit-delay 1s -delay 300ms                    (1 run, 240 s timeout)
./bench.sh rerun_bt   # re-ran the Bubble Tea rows after fixing textarea focus (see caveats)
./bench.sh batchwin   # Bubble Tea with -batch -batchwin 16ms
python3 summarize.py > results/tables.md
go test ./...  &&  go test -tags fastview ./cmd/bt/     # golden / teatest / SimulationScreen / pty+VT e2e
```

Raw data: `results/*.jsonl` (one JSON object per run, prefixed with the variant label). `results/run1/` holds the first full pass; its Bubble Tea and tview heap numbers are invalid because the model was unreachable at GC time. `results/tables.md` is generated from the raw data.

### Results

Column notes:
- CPU = user+sys of the whole process lifetime, including ~0.3 s start delay and 1 s linger after the final frame.
- "frames/View calls" = frames drawn. For Bubble Tea it is the number of `View()` calls; terminal flushes are capped at 60/s.
- "producer wall" = time the producer needed to hand over all events. A value above N/rate means the UI back-pressured the agent.
- "DONE on screen after last event" = harness timestamp of `DONE` minus the producer's last-send timestamp.

### Binary size (bytes)

| spike | default build | -ldflags "-s -w" |
| --- | --- | --- |
| Bubble Tea v2 + bubbles viewport | 6.96 MB | 4.88 MB |
| Bubble Tea v2 + windowed transcript | 6.96 MB | 4.88 MB |
| tview (tcell v2) | 5.77 MB | 3.98 MB |
| tcell v3 raw | 4.76 MB | 3.17 MB |
| vaxis | 4.68 MB | 3.14 MB |
| ultraviolet direct (TerminalScreen) | 6.16 MB | 4.25 MB |
| awesome-gocui | 5.45 MB | 3.68 MB |

### Cold start (stripped binaries, 20 runs after 1 warm-up, pty 120x40)

| spike | first byte median ms | first full frame median ms | p10–p90 full ms | first full frame, silent terminal ms |
| --- | --- | --- | --- | --- |
| Bubble Tea v2 + bubbles viewport | 23.52 | 24.43 | 23.4–25.0 | 22 |
| Bubble Tea v2 + windowed transcript | 23.13 | 23.88 | 23.5–24.9 | 22 |
| tview (tcell v2) | 3.79 | 4.56 | 4.0–5.4 | 5 |
| tcell v3 raw | 3.05 | 3.35 | 3.1–3.7 | 1,008 |
| vaxis | 2.79 | 3.17 | 3.0–3.4 | 3,013 |
| ultraviolet direct (TerminalScreen) | 3.25 | 3.85 | 3.6–4.3 | 6 |
| awesome-gocui | 3.28 | 4.39 | 4.1–4.8 | 7 |

### Idle (5 s window starting 1 s after first frame)

| spike | CPU ms in 5 s | RSS KB | phys footprint KB |
| --- | --- | --- | --- |
| Bubble Tea v2 + bubbles viewport | 34.2 | 12,704 | 9,376 |
| Bubble Tea v2 + windowed transcript | 29.1 | 12,560 | 9,184 |
| tview (tcell v2) | 0 | 8,960 | 6,320 |
| tcell v3 raw | 0 | 7,920 | 5,840 |
| vaxis | 0 | 6,992 | 4,752 |
| ultraviolet direct (TerminalScreen) | 0 | 11,552 | 8,912 |
| awesome-gocui | 12.9 | 11,408 | 8,960 |

### Streaming: 10,000 events at 200/s (50 s)

| spike | runs | CPU ms (user+sys) | max RSS KB | RSS after done KB | bytes to pty | bytes/event | frames/View calls | producer wall ms | DONE on screen after last event ms | heap after GC KB |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Bubble Tea v2 + bubbles viewport | 1 | 40,046 | 20,800 | 20,656 | 1,271,321 | 127 | 10,008 | 51,339 | 12 | 1,745 |
| Bubble Tea v2 + viewport, batched msgs | 1 | 40,303 | 20,848 | 20,688 | 1,261,612 | 126 | 9,751 | 49,996 | 22 | 1,758 |
| Bubble Tea v2 + windowed transcript | 1 | 9,347 | 20,544 | 20,368 | 1,261,616 | 126 | 10,008 | 49,996 | 5 | 1,763 |
| Bubble Tea v2 + windowed, batched msgs | 1 | 7,166 | 20,896 | 20,752 | 1,261,513 | 126 | 10,002 | 49,995 | 6 | 1,758 |
| Bubble Tea v2 + viewport, 16 ms batch window | 1 | 13,730 | 20,736 | 20,576 | 1,199,220 | 120 | 2,508 | 49,996 | 22 | 1,757 |
| Bubble Tea v2 + windowed, 16 ms batch window | 1 | 4,202 | 20,544 | 20,400 | 1,199,516 | 120 | 2,508 | 49,996 | 5 | 1,728 |
| tview (tcell v2) | 1 | 6,880 | 21,328 | 21,152 | 15,647,266 | 1,565 | 2,476 | 49,995 | 20 | 3,682 |
| tcell v3 raw | 1 | 4,010 | 14,240 | 14,016 | 13,244,515 | 1,324 | 2,946 | 49,995 | 17 | 1,680 |
| vaxis | 1 | 2,231 | 12,096 | 11,824 | 12,497,242 | 1,250 | 2,948 | 49,995 | 17 | 2,315 |
| ultraviolet direct (TerminalScreen) | 1 | 5,210 | 19,664 | 19,472 | 10,021,736 | 1,002 | 2,948 | 49,996 | 15 | 3,226 |
| ultraviolet direct (TerminalRenderer + scroll optim) | 1 | 3,972 | 18,144 | 17,952 | 971,806 | 97 | 2,949 | 49,996 | 15 | 2,668 |
| awesome-gocui | 1 | 6,640 | 63,296 | 63,072 | 13,620,890 | 1,362 | 2,502 | 49,995 | 9 | 24,981 |

### Burst: 10,000 events as fast as possible (median of 3)

| spike | runs | CPU ms (user+sys) | max RSS KB | RSS after done KB | bytes to pty | bytes/event | frames/View calls | producer wall ms | DONE on screen after last event ms | heap after GC KB |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Bubble Tea v2 + bubbles viewport | 3 | 34,040 | 20,704 | 20,544 | 1,155,391 | 116 | 10,008 | 32,747 | 14 | 1,740 |
| Bubble Tea v2 + viewport, batched msgs | 3 | 139 | 18,768 | 18,304 | 19,996 | 2 | 20 | 71 | 30 | 1,694 |
| Bubble Tea v2 + windowed transcript | 3 | 1,215 | 21,024 | 20,848 | 176,215 | 18 | 10,008 | 924 | 15 | 1,723 |
| Bubble Tea v2 + windowed, batched msgs | 3 | 83 | 18,560 | 18,208 | 10,446 | 1 | 21 | 37 | 14 | 1,695 |
| Bubble Tea v2 + windowed, 16 ms batch window | 3 | 73 | 19,056 | 18,672 | 4,041 | 0 | 9 | 10 | 41 | 1,689 |
| tview (tcell v2) | 3 | 136 | 17,104 | 16,880 | 24,625 | 2 | 5 | 80 | 44 | 3,551 |
| tcell v3 raw | 3 | 35 | 12,160 | 11,488 | 12,380 | 1 | 4 | 10 | 8 | 1,637 |
| vaxis | 3 | 42 | 11,968 | 11,264 | 11,338 | 1 | 4 | 15 | 9 | 2,301 |
| ultraviolet direct (TerminalScreen) | 3 | 51 | 16,176 | 15,872 | 8,648 | 1 | 5 | 22 | 14 | 3,170 |
| ultraviolet direct (TerminalRenderer + scroll optim) | 3 | 48 | 15,760 | 15,424 | 6,258 | 1 | 5 | 26 | 9 | 2,633 |
| awesome-gocui | 3 | 96 | 51,584 | 51,280 | 12,944 | 1 | 4 | 12 | 52 | 24,939 |

### Burst, no frame coalescing (-fps 0: draw after every event)

| spike | runs | CPU ms (user+sys) | max RSS KB | RSS after done KB | bytes to pty | bytes/event | frames/View calls | producer wall ms | DONE on screen after last event ms | heap after GC KB |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| tview (tcell v2) | 1 | 10,386 | 19,952 | 19,776 | 63,112,756 | 6,311 | 10,002 | 9,618 | 0 | 3,562 |
| tcell v3 raw | 1 | 4,128 | 14,576 | 14,352 | 48,577,934 | 4,858 | 10,002 | 3,572 | 93 | 1,672 |
| vaxis | 1 | 1,627 | 12,528 | 12,240 | 45,700,297 | 4,570 | 10,002 | 1,548 | 169 | 2,308 |
| ultraviolet direct (TerminalScreen) | 1 | 4,301 | 19,600 | 19,408 | 33,931,450 | 3,393 | 10,002 | 3,696 | 1 | 3,200 |
| ultraviolet direct (TerminalRenderer + scroll optim) | 1 | 3,385 | 18,560 | 18,368 | 1,507,666 | 151 | 10,002 | 2,694 | 0 | 2,662 |
| awesome-gocui | 1 | 284 | 66,720 | 66,320 | 13,662 | 1 | 5 | 21 | 33 | 29,450 |

### Transcript growth: 50,000 events burst, all lines retained

| spike | runs | CPU ms (user+sys) | max RSS KB | RSS after done KB | bytes to pty | bytes/event | frames/View calls | producer wall ms | DONE on screen after last event ms | heap after GC KB |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Bubble Tea v2 + bubbles viewport (TIMED OUT) | 1 | 245,231 | 25,008 | – | 4,277,743 | 86 | – | – | – | – |
| Bubble Tea v2 + viewport, batched msgs | 1 | 931 | 27,072 | 26,880 | 130,498 | 3 | 58 | 766 | 101 | 6,028 |
| Bubble Tea v2 + windowed transcript | 1 | 5,744 | 30,048 | 29,904 | 843,944 | 17 | 50,008 | 4,482 | 3 | 6,121 |
| Bubble Tea v2 + windowed, batched msgs | 1 | 185 | 26,928 | 26,656 | 26,263 | 1 | 58 | 124 | 10 | 6,101 |
| Bubble Tea v2 + windowed, 16 ms batch window | 1 | 198 | 31,584 | 31,344 | 10,530 | 0 | 11 | 61 | 73 | 5,979 |
| tview (tcell v2) | 1 | 477 | 36,688 | 36,480 | 62,755 | 1 | 11 | 384 | 38 | 15,677 |
| tcell v3 raw | 1 | 96 | 19,552 | 19,328 | 23,338 | 0 | 6 | 42 | 9 | 6,488 |
| vaxis | 1 | 111 | 20,320 | 20,096 | 25,064 | 1 | 7 | 54 | 15 | 7,157 |
| ultraviolet direct (TerminalScreen) | 1 | 121 | 24,896 | 24,384 | 19,120 | 0 | 8 | 69 | 16 | 8,026 |
| ultraviolet direct (TerminalRenderer + scroll optim) | 1 | 106 | 24,992 | 24,448 | 11,882 | 0 | 7 | 59 | 10 | 7,486 |
| awesome-gocui | 1 | 244 | 207,456 | 207,200 | 12,951 | 0 | 4 | 47 | 102 | 122,290 |


Extra cold-start data point: `bin/harness -bin bin/bt.stripped -args "-fps 120" -cold -repeat 20` gives a median first full frame of **14.8 ms** (`results/cold_bt_fps120.txt`).

### Reading the numbers

1. **Cold start.**
   - Every non-Charm option paints in 3–5 ms. That time is essentially exec + raw mode + one write.
   - Bubble Tea v2 paints at ~24 ms. Process init is not the cause (GODEBUG=inittrace shows under 1 ms of package init for all spikes). The first frame is written on the first tick of the 60 fps renderer ticker.
   - All options pay a one-off ~200–300 ms on the *first* exec of a freshly built binary. This is the macOS code-signing / syspolicy scan, and it is excluded by the warm-up run.
   - With a terminal that never answers queries, vaxis waits 3 s (DA1 timeout) and tcell v3 waits 1 s before the first frame. Bubble Tea, Ultraviolet, tview and gocui do not block.
2. **Idle.**
   - Bubble Tea's frame ticker runs forever: ~6 ms CPU/s, about 0.6% of one core, with ~50 wake-ups/s. gocui also has a small idle cost (~2.5 ms/s).
   - tcell, vaxis, Ultraviolet and tview use 0 CPU while idle.
   - Idle RSS: vaxis 7.0 MB < tcell 7.9 < tview 9.0 < gocui 11.4 ≈ Ultraviolet 11.6 < Bubble Tea 12.6.
3. **Steady streaming (200 ev/s for 50 s).**
   - No option dropped events, and none back-pressured the producer: producer wall time was 49.99 s everywhere except naive Bubble Tea (51.3 s).
   - The final frame reached the pty 5–22 ms after the last event in every case.
   - CPU differences therefore come only from per-event and per-frame work:
     - vaxis: 2.2 s
     - Bubble Tea windowed + 16 ms batching: 4.2 s
     - Ultraviolet with hard scroll: 4.0 s
     - tcell: 4.0 s
     - Ultraviolet `TerminalScreen`: 5.2 s
     - gocui: 6.6 s
     - tview: 6.9 s
     - Bubble Tea windowed, one message per event: 7–9 s (`View()` runs 10,000 times)
     - Bubble Tea + bubbles viewport: **40 s**, i.e. 80% of a core at the end of the run. It grows with transcript length.
   - These are single runs. Repeat runs of the same Bubble Tea binary varied 6.5–9.3 s, so treat differences under ~30% as noise.
4. **Renderer efficiency (bytes to the terminal).**
   - Bubble Tea, and Ultraviolet driven through `TerminalRenderer` with scroll optimisation, write **~100–125 bytes per event**.
   - tcell, vaxis, tview, gocui and Ultraviolet `TerminalScreen` rewrite every shifted row when the transcript scrolls, so they write **1,000–1,600 bytes per event**: 10–16 MB per 10k events vs 1.0–1.3 MB.
   - This matters for SSH, tmux, slow terminal emulators and terminal CPU. It shows even more without frame coalescing (`-fps 0`): 34–63 MB vs 1.5 MB.
5. **Bursts (10k events as fast as possible).**
   - With batching, Bubble Tea handles the burst in 70–140 ms CPU and ~20 View calls. That is in the same range as tcell and vaxis (35–42 ms) and tview (136 ms).
   - Without batching, one `Send` + `Update` + `View` per event throttles the producer to ~11k events/s: 0.9 s producer wall time for the windowed transcript. The naive viewport version takes 33 s and stalls the agent for all of that time.
6. **Transcript growth (50k lines retained).**
   - Max RSS:
     - tcell 19.6 MB, vaxis 20.3 MB, Ultraviolet 25 MB
     - Bubble Tea 27–32 MB, tview 36.7 MB
     - **gocui 207 MB**: it stores every line as a grid of cell structs, ~2.5 KB/line
   - Live heap after GC: Bubble Tea ~6.0 MB (pre-rendered ANSI strings ~120 B/line), tcell 6.5 MB, vaxis 7.2 MB, Ultraviolet 7.5–8.0 MB, tview 15.7 MB.
   - Naive bubbles viewport: **did not finish in 240 s** (245 s CPU, still at 100% of a core).

### Measurement caveats

- **Terminal emulation.** The pty harness is not a real terminal emulator: it answers queries but does not render, so terminal-side cost (parsing 12 MB vs 1.2 MB) is not measured. Byte counts are the proxy.
- **Query answers.** The harness answers "supported" only for mode 2026. Mode 2027, kitty keyboard, explicit width and so on are reported as unsupported, which exercises the fallback paths (e.g. wcwidth instead of grapheme width).
- **Timing.**
  - Wall-clock latencies compare the app's clock (producer end) with the harness's clock (pty read). Both are on the same host, and resolution is ~1 ms.
  - "DONE on screen" includes the ≤16.7 ms frame interval.
- **CPU.**
  - CPU is whole-process rusage. It includes startup, the 300 ms pre-delay and the 1 s linger, which is ~6 ms of ticker CPU for Bubble Tea and 0 for the others.
  - Streaming runs are single runs, bursts are medians of 3, and cold starts are medians of 20 after a discarded warm-up.
  - Other processes were running on the laptop. Single-run CPU for Bubble Tea varied by ~30% between passes.
- **RSS** depends on GC pacing (default GOGC=100, no GOMEMLIMIT). The "heap after GC" column separates live data from GC slack. In `results/run1/`, the Bubble Tea and tview heap values are invalid because the model was unreachable; this was fixed with `runtime.KeepAlive` and re-run.
- **Mid-benchmark fix.**
  - The Bubble Tea spike first focused the textarea in `Init()`. `Init` has a value receiver, so the focus was lost. This is a classic Bubble Tea gotcha.
  - Bubble Tea rows were re-run after the fix (`./bench.sh rerun_bt`). The other rows come from the pass before.
- **Frame counts.** "frames/View calls" counts `View()` calls for Bubble Tea, not flushes. The actual flush rate is capped by the 60 fps ticker.
- **Same design, different idioms.**
  - All spikes cap at 60 fps and render only visible lines, except the deliberate "idiomatic viewport" Bubble Tea variant.
  - tview (TextView) and gocui (view buffer) store and wrap text their own way. This is part of what is being measured.
  - Styling uses each library's native style type: tview colour tags, gocui ANSI SGR, lipgloss for Bubble Tea.
- **Not covered.** No Windows or Linux numbers were taken. Ultraviolet is an untagged pseudo-version and may change.

## 3. Developer experience for this use case

| | Bubble Tea v2 | tview | tcell v3 raw | vaxis | Ultraviolet direct | gocui |
|---|---|---|---|---|---|---|
| Spike size (non-blank, non-comment Go lines; shared `internal/sim` = 292 not counted) | 247 (incl. both transcript variants + batching) | 119 | 164 | 145 | 196 (incl. `-hardscroll` path) | 157 |
| **External events** | `p.Send(msg)`. It is unbuffered and blocks until the loop takes it, so a slow `Update`/`View` back-pressures the agent. Batch in the adapter (drain queue / 16 ms window). | `QueueUpdate(func)` + you own the draw scheduling; `QueueUpdateDraw` per event = a full redraw per event (10 s CPU / 10k) | Your own channel in a `select` with `EventQ()`. The cleanest model. | `PostEventBlocking` (the documented `PostEvent` **drops** on a full queue) or `SyncFunc` | `Terminal.SendEvent` (blocking) or your own channel | `Update(func)` per event; internally coalesces |
| **Transcript / viewport** | `bubbles/viewport` does not scale for append-heavy logs (O(n) per update). Write a virtualised list (as Crush does). | `TextView` works to 50k lines (15.7 MB heap), with colour-tag markup and built-in scrolling, but all text is re-parsed on width change | none: ~40 lines of your own | `widgets/pager`, `vxfw/list`; or your own | none: your own | view with autoscroll; memory-hungry |
| **Multi-line input** | `bubbles/textarea` (selection, word ops, paste, dynamic height, real cursor) | `TextArea` (good) | none; the spike has a toy | `widgets/textinput` and `vxfw/textfield` are **single-line** | none | editable view (basic) |
| **Lists / completion popups** (slash commands) | `bubbles/list` (fuzzy filter), `table`, `help`, `key` bindings, spinner, progress; plus community (overlays) | `List`, `Table`, `Modal`, `Form`, `Pages` | none | `vxfw/list`, `widgets/list` | none | none |
| **Markdown** | **glamour v2** (`charm.land/glamour/v2`); chroma highlighting | none built-in (convert to tview tags yourself) | none | none (can print ANSI from glamour through its SGR parser, but not a native fit) | glamour output drawn via `uv.NewStyledString(...).Draw` | none |
| **Styling** | lipgloss v2: borders, padding, layout joins, adaptive light/dark, colour downsampling | `[fg:bg:attr]` colour tags + tcell styles | `tcell.Style` | `vaxis.Style` structs, hyperlinks, styled underlines | `uv.Style` + lipgloss strings | ANSI SGR in text |
| **Testability** | Best: `Update`/`View` are pure, so golden-test `View().Content` with no terminal (`cmd/bt/model_test.go` → `golden.RequireEqual`). `teatest/v2` (`github.com/charmbracelet/x/exp/teatest/v2`, **x/exp pseudo-version only**) drives a real `tea.Program`. | tcell v2 `SimulationScreen` via `app.SetScreen` (`cmd/tview/ui_test.go`); needs a small refactor to inject the screen; `SetScreen` calls `Init`, which resets the size | v3 **removed SimulationScreen**; `vt.MockTerm` is internal-only. Use pty + VT emulator (`harness/e2e_test.go`) or test your own widget code against a `Screen` interface. | No headless screen; `Options.WithConsole` allows a fake console. pty + VT emulator works. | Draw code targets the `uv.Screen` interface, so render into `uv.NewScreenBuffer` in tests. pty + VT for e2e. | pty + VT only |
| **Framework-agnostic e2e** | `harness/e2e_test.go`: pty + `charmbracelet/x/vt` replay; passes for all 7 binaries | same | same | same | same | same |
| **Gotchas found** | value-receiver `Init` loses textarea focus; `View()` per message; 60 fps ticker never sleeps; first frame gated by the ticker | status/transcript must be touched only on the UI goroutine; `Draw()` from another goroutine races | you build everything (textarea, wrap, scroll, selection) | `PostEvent` drops events; startup blocks on DA1 (3 s if unanswered); `ev.EventType` release events with kitty kbd | `TerminalScreen` lacks scroll optimisation (need `TerminalRenderer`); API unstable | 2022 release; 2.5 KB/line |

## 4. Recommendation

Ranking for uagent's live-run TUI, weighing resource use, cold start, rendering efficiency and maintainability:

1. **Bubble Tea v2 + our own virtualised transcript + event batching.** Recommended.
   - Rendering efficiency is the best measured: the scroll-region renderer writes 10x fewer bytes than every tcell- or vaxis-based option. This matters over SSH and in tmux, and it keeps the terminal emulator cheap.
   - Streaming CPU with batching is in the same band as tcell and Ultraviolet (≈4 s per 10k events at 200/s, ~8% of a core) and within ~2x of vaxis.
   - It has the ecosystem this app needs: textarea, list (slash-command completion), glamour markdown, lipgloss layout, pure-function golden tests, and Crush as a production precedent for exactly this app shape.
   - The costs (+20 ms first paint, ~0.6% of a core while idle, +5–8 MB RSS, +1.7 MB binary) are real but immaterial for an interactive agent session.
   - Required discipline:
     - (a) Never `SetContent*` a growing transcript. Keep a slice of items, cache each item's rendered lines keyed by width, and join only the visible window.
     - (b) Convert agent events to one `tea.Msg` per ≤16 ms window, or drain what is queued. Never one `Send` per event.
     - (c) Real cursor (`SetVirtualCursor(false)`).
     - (d) Optionally `WithFPS(120)` for a faster first paint.
     - (e) Keep heavy work (markdown rendering, diffing) out of `View()`. Render on arrival and cache.
2. **Ultraviolet direct (Charm hybrid)** is the fallback if profiling ever shows the Elm loop matters.
   - It uses the same renderer, lipgloss and glamour strings (drawn with `uv.NewStyledString`), and the same input decoder, without `View()`-per-message.
   - With `TerminalRenderer` + scroll optimisation it matched the best CPU band (4.0 s) with the lowest bytes of all (97 B/event).
   - Downsides: untagged, "API may change", and `TerminalScreen` does not enable hard scroll. We would own every widget, including the textarea.
3. **vaxis** is the most frugal and the most modern.
   - Numbers: 2.2 s CPU at 200/s, 12 MB RSS, 3 ms cold start, zero idle CPU.
   - Features: best terminal support (kitty keyboard, graphics, mode 2027, explicit width, inline mode, embedded terminal widget).
   - Choose it only if CPU and RSS are hard requirements (e.g. many concurrent sessions on a small box), and accept the following:
     - the bus factor: a single maintainer and ~100 stars; aerc is the main user;
     - a thin widget set, so we write the textarea and markdown ourselves;
     - no scroll-region optimisation (12 MB per 10k events);
     - a 3 s stall on terminals that do not answer DA1.
4. **tcell v3 raw.**
   - Strengths: solid, well maintained, adopted by lazygit, low CPU and RSS.
   - Weaknesses: no widgets (tview has not moved to v3), no scroll optimisation, a 1 s stall on silent terminals, and v3 dropped `SimulationScreen`, which makes testing harder.
   - vaxis dominates it on resource use, and Bubble Tea or Ultraviolet dominate it on bytes written.
5. **tview.**
   - Productive for forms and tables, and it has `TextView` and `TextArea`.
   - But it is stuck on tcell v2, uses 1.6x Bubble Tea's CPU at 200/s, and writes 13x the bytes. It has the highest non-gocui memory at 50k lines (37 MB), and its colour-tag styling is weaker for markdown or diff rendering.
6. **gocui (awesome-gocui).** Not recommended: its last release is from 2022, it uses ~2.5 KB per transcript line (207 MB at 50k lines), and it has no textarea.

**Plain answer:** Bubble Tea v2 is good enough, and it is the right default. No other option is *materially* better for this app once the transcript is virtualised and events are batched: vaxis saves ~2 s CPU per 10k events and ~8 MB RSS but costs 10x the terminal bytes and most of the widget work. Bubble Tea v2 is **not** good enough if you use it naively (bubbles viewport + one message per event): that version burns 80% of a core at 200 ev/s and cannot hold 50k lines.

Switch away only if one of these becomes a requirement:
- sub-10 ms cold start, e.g. the TUI is spawned per command;
- zero idle wake-ups, e.g. battery-sensitive, long-idle sessions;
- sub-15 MB RSS per session at scale.

In those cases the hybrid is an Ultraviolet-direct core with our own widgets if we want to stay in the Charm ecosystem (cheapest migration), or vaxis if maximum efficiency and terminal features outweigh ecosystem and bus-factor risk.

## Files

- `internal/sim/sim.go`: shared flags, event generator, producer, counters, input model, throttle, stats
- `cmd/bt/` (Bubble Tea v2; `-tags fastview` for the windowed transcript; `-batch`, `-batchwin`), `cmd/tview/`, `cmd/tcell/`, `cmd/vaxis/`, `cmd/uv/` (`-hardscroll`), `cmd/gocui/`
- `cmd/bt/model_test.go` (golden + teatest/v2), `cmd/tview/ui_test.go` (SimulationScreen), `harness/e2e_test.go` (pty + VT emulator, all spikes)
- `harness/`: pty benchmark harness (`rusage_darwin.go` uses cgo `proc_pid_rusage`); `replay/`: dump → final screen
- `bench.sh`, `summarize.py`, `results/` (raw JSONL, `tables.md`, `sizes.txt`, `cold_bt_fps120.txt`)
