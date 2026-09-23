#!/usr/bin/env bash
# Runs every benchmark sequentially. Results: results/*.jsonl (one JSON object
# per run, produced by bin/harness). Summarise with: python3 summarize.py
set -euo pipefail
cd "$(dirname "$0")"
mkdir -p bin results
R=results
H=./bin/harness

build() {
  go build -o bin/harness ./harness
  go build -o bin/replay ./replay
  for s in tcell vaxis uv tview gocui; do
    go build -o bin/$s ./cmd/$s
    go build -ldflags "-s -w" -o bin/$s.stripped ./cmd/$s
  done
  go build -o bin/bt ./cmd/bt
  go build -ldflags "-s -w" -o bin/bt.stripped ./cmd/bt
  go build -tags fastview -o bin/btfast ./cmd/bt
  go build -tags fastview -ldflags "-s -w" -o bin/btfast.stripped ./cmd/bt
}

sizes() {
  : > $R/sizes.txt
  for b in bt btfast tview tcell vaxis uv gocui; do
    printf "%s %s %s\n" $b $(stat -f %z bin/$b) $(stat -f %z bin/$b.stripped) >> $R/sizes.txt
  done
}

SPIKES="bt btfast tview tcell vaxis uv gocui"

cold() {
  : > $R/cold.jsonl
  for b in $SPIKES; do
    $H -bin ./bin/$b.stripped -cold -repeat 1 > /dev/null   # warm-up: first exec pays macOS code-signing/syspolicy scan
    $H -bin ./bin/$b.stripped -cold -repeat 20 | sed "s/^/$b /" >> $R/cold.jsonl
  done
  # Terminal that never answers queries (e.g. some multiplexers / dumb pipes)
  : > $R/cold_silent.jsonl
  for b in $SPIKES; do
    $H -bin ./bin/$b.stripped -cold -respond=false -repeat 3 -timeout 10s | sed "s/^/$b /" >> $R/cold_silent.jsonl
  done
}

idle() {
  : > $R/idle.jsonl
  for b in $SPIKES; do
    $H -bin ./bin/$b.stripped -idle 5s | sed "s/^/$b /" >> $R/idle.jsonl
  done
}

# name, args, repeat, timeout, spikes...
stream() {
  local name=$1 args=$2 rep=$3 to=$4; shift 4
  for b in "$@"; do
    local bin=${b%%:*} extra=""
    [[ $b == *:* ]] && extra=${b#*:} && extra=${extra//_/ }
    local label=$b
    for i in $(seq 1 $rep); do
      $H -bin ./bin/$bin.stripped -timeout $to -stats /tmp/tuibench.stats.json \
        -args "$args $extra -stats /tmp/tuibench.stats.json" | sed "s/^/$label /" >> $R/$name.jsonl
    done
  done
}

run_streams() {
  rm -f $R/rate200.jsonl $R/burst10k.jsonl $R/burst10k_fps0.jsonl $R/lines50k.jsonl
  local ALL="bt bt:-batch btfast btfast:-batch tview tcell vaxis uv uv:-hardscroll gocui"
  stream rate200 "-n 10000 -rate 200 -exit -exit-delay 1s -delay 300ms" 1 120s $ALL
  stream burst10k "-n 10000 -exit -exit-delay 1s -delay 300ms" 3 180s $ALL
  stream burst10k_fps0 "-n 10000 -exit -exit-delay 1s -delay 300ms -fps 0" 1 180s tview tcell vaxis uv uv:-hardscroll gocui
  stream lines50k "-n 50000 -exit -exit-delay 1s -delay 300ms" 1 240s bt:-batch btfast btfast:-batch tview tcell vaxis uv uv:-hardscroll gocui
  stream lines50k "-n 50000 -exit -exit-delay 1s -delay 300ms" 1 240s bt
}

# Re-run only the Bubble Tea variants (after a change to cmd/bt) and replace
# their rows in the stream result files.
rerun_bt() {
  for f in rate200 burst10k lines50k; do
    [ -f $R/$f.jsonl ] && grep -v -E '^(bt|btfast)(:-batch)? ' $R/$f.jsonl > $R/$f.tmp && mv $R/$f.tmp $R/$f.jsonl
  done
  stream rate200 "-n 10000 -rate 200 -exit -exit-delay 1s -delay 300ms" 1 120s bt bt:-batch btfast btfast:-batch
  stream burst10k "-n 10000 -exit -exit-delay 1s -delay 300ms" 3 180s bt bt:-batch btfast btfast:-batch
  stream lines50k "-n 50000 -exit -exit-delay 1s -delay 300ms" 1 240s bt:-batch btfast btfast:-batch bt
}

# Bubble Tea with time-window batching (one Update/View per 16 ms window).
batchwin() {
  for f in rate200 burst10k lines50k; do
    [ -f $R/$f.jsonl ] && grep -v -E '^(bt|btfast):-batch_-batchwin ' $R/$f.jsonl > $R/$f.tmp && mv $R/$f.tmp $R/$f.jsonl
  done
  stream rate200 "-n 10000 -rate 200 -exit -exit-delay 1s -delay 300ms" 1 120s "bt:-batch_-batchwin_16ms" "btfast:-batch_-batchwin_16ms"
  stream burst10k "-n 10000 -exit -exit-delay 1s -delay 300ms" 3 180s "btfast:-batch_-batchwin_16ms"
  stream lines50k "-n 50000 -exit -exit-delay 1s -delay 300ms" 1 240s "btfast:-batch_-batchwin_16ms"
}

case "${1:-all}" in
  build) build ;;
  sizes) sizes ;;
  cold) cold ;;
  idle) idle ;;
  streams) run_streams ;;
  rerun_bt) rerun_bt ;;
  batchwin) batchwin ;;
  all) build; sizes; cold; idle; run_streams ;;
esac
