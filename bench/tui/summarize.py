#!/usr/bin/env python3
"""Summarise results/*.jsonl into markdown tables (printed to stdout)."""
import json, os, re, statistics as st

R = os.path.join(os.path.dirname(os.path.abspath(__file__)), "results")
ORDER = ["bt", "bt:-batch", "btfast", "btfast:-batch", "bt:-batch_-batchwin_16ms", "btfast:-batch_-batchwin_16ms", "tview", "tcell", "vaxis", "uv", "uv:-hardscroll", "gocui"]
NAMES = {
    "bt": "Bubble Tea v2 + bubbles viewport",
    "bt:-batch": "Bubble Tea v2 + viewport, batched msgs",
    "btfast": "Bubble Tea v2 + windowed transcript",
    "btfast:-batch": "Bubble Tea v2 + windowed, batched msgs",
    "bt:-batch_-batchwin_16ms": "Bubble Tea v2 + viewport, 16 ms batch window",
    "btfast:-batch_-batchwin_16ms": "Bubble Tea v2 + windowed, 16 ms batch window",
    "tview": "tview (tcell v2)",
    "tcell": "tcell v3 raw",
    "vaxis": "vaxis",
    "uv": "ultraviolet direct (TerminalScreen)",
    "uv:-hardscroll": "ultraviolet direct (TerminalRenderer + scroll optim)",
    "gocui": "awesome-gocui",
}


def load(name):
    p = os.path.join(R, name)
    out = {}
    if not os.path.exists(p):
        return out
    for line in open(p):
        line = line.strip()
        if not line or " SUMMARY" in line or line.startswith("SUMMARY"):
            continue
        label, js = line.split(" ", 1)
        try:
            d = json.loads(js)
        except Exception:
            continue
        if isinstance(d.get("stats"), dict):
            pass
        out.setdefault(label, []).append(d)
    return out


def med(xs):
    xs = [x for x in xs if x is not None]
    return st.median(xs) if xs else None


def f(x, nd=1):
    if x is None:
        return "–"
    if isinstance(x, float):
        return f"{x:,.{nd}f}"
    return f"{x:,}"


def row(cells):
    return "| " + " | ".join(cells) + " |"


def sizes():
    print("### Binary size (bytes)\n")
    print(row(["spike", "default build", "-ldflags \"-s -w\""]))
    print(row(["---"] * 3))
    for line in open(os.path.join(R, "sizes.txt")):
        n, a, b = line.split()
        print(row([NAMES.get(n, n), f"{int(a)/1e6:.2f} MB", f"{int(b)/1e6:.2f} MB"]))
    print()


def cold():
    runs = load("cold.jsonl")
    silent = load("cold_silent.jsonl")
    print("### Cold start (stripped binaries, 20 runs after 1 warm-up, pty 120x40)\n")
    print(row(["spike", "first byte median ms", "first full frame median ms", "p10–p90 full ms", "first full frame, silent terminal ms"]))
    print(row(["---"] * 5))
    for k in ["bt", "btfast", "tview", "tcell", "vaxis", "uv", "gocui"]:
        rs = runs.get(k, [])
        if not rs:
            continue
        full = sorted(r["first_full_ms"] for r in rs)
        p10, p90 = full[len(full) // 10], full[(len(full) * 9) // 10 - 1]
        sil = med([r["first_full_ms"] for r in silent.get(k, [])])
        print(row([NAMES[k], f(med([r["first_byte_ms"] for r in rs]), 2), f(med(full), 2), f"{p10:.1f}–{p90:.1f}", f(sil, 0)]))
    print()


def idle():
    runs = load("idle.jsonl")
    print("### Idle (5 s window starting 1 s after first frame)\n")
    print(row(["spike", "CPU ms in 5 s", "RSS KB", "phys footprint KB"]))
    print(row(["---"] * 4))
    for k in ["bt", "btfast", "tview", "tcell", "vaxis", "uv", "gocui"]:
        for r in runs.get(k, []):
            print(row([NAMES[k], f(r["idle_cpu_ms"], 1), f(r["idle_rss_kb"]), f(r.get("idle_footprint_kb"))]))
    print()


def stream(fname, title, note=""):
    runs = load(fname)
    if not runs:
        return
    print(f"### {title}\n")
    if note:
        print(note + "\n")
    print(row(["spike", "runs", "CPU ms (user+sys)", "max RSS KB", "RSS after done KB", "bytes to pty", "bytes/event",
               "frames/View calls", "producer wall ms", "DONE on screen after last event ms", "heap after GC KB"]))
    print(row(["---"] * 11))
    for k in ORDER:
        rs = runs.get(k)
        if not rs:
            continue
        timed_out = any(r.get("timed_out") for r in rs)
        stats = [r.get("stats") or {} for r in rs]
        n = int(re.search(r"-n (\d+)", rs[0]["args"]).group(1))
        cpu = med([r["user_ms"] + r["sys_ms"] for r in rs])
        prod = med([(s["prod_end_ns"] - s["prod_start_ns"]) / 1e6 for s in stats if s.get("prod_end_ns")])
        lag = med([(r["done_seen_unix_ns"] - s["prod_end_ns"]) / 1e6 for r, s in zip(rs, stats) if s.get("prod_end_ns") and r.get("done_seen_unix_ns")])
        frames = med([s.get("frames") for s in stats if s])
        heap = med([s.get("heap_alloc_after_gc", 0) / 1024 for s in stats if s])
        byts = med([r["bytes"] for r in rs])
        label = NAMES[k] + (" (TIMED OUT)" if timed_out else "")
        print(row([label, str(len(rs)), f(cpu, 0), f(med([r["maxrss_kb"] for r in rs])), f(med([r.get("after_done_rss_kb") for r in rs])),
                   f(byts), f(byts / n if byts else None, 0), f(frames), f(prod, 0), f(lag, 0), f(heap, 0)]))
    print()


if __name__ == "__main__":
    sizes()
    cold()
    idle()
    stream("rate200.jsonl", "Streaming: 10,000 events at 200/s (50 s)")
    stream("burst10k.jsonl", "Burst: 10,000 events as fast as possible (median of 3)")
    stream("burst10k_fps0.jsonl", "Burst, no frame coalescing (-fps 0: draw after every event)")
    stream("lines50k.jsonl", "Transcript growth: 50,000 events burst, all lines retained")
