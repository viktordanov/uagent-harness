// Package sim holds the framework-independent parts shared by every spike:
// flags, the synthetic event producer, the transcript line model, a tiny
// multi-line input model (for the frameworks without a textarea widget) and
// the stats file written at exit.
package sim

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// Config is parsed from flags; identical for every spike.
type Config struct {
	N         int           // number of events to emit
	Rate      float64       // events per second; 0 = as fast as the app accepts them
	Exit      bool          // exit automatically after the burst has been rendered
	ExitDelay time.Duration // how long to keep running after the final frame
	Stats     string        // path of a JSON stats file written on exit
	FPS       int           // max frames per second (0 = draw after every event, raw spikes only)
	Delay     time.Duration // delay before the producer starts
}

func ParseFlags() Config {
	var c Config
	flag.IntVar(&c.N, "n", 0, "number of synthetic events")
	flag.Float64Var(&c.Rate, "rate", 0, "events per second (0 = unthrottled burst)")
	flag.BoolVar(&c.Exit, "exit", false, "exit after the burst completes")
	flag.DurationVar(&c.ExitDelay, "exit-delay", 150*time.Millisecond, "linger after final frame before exiting")
	flag.StringVar(&c.Stats, "stats", "", "write JSON stats here on exit")
	flag.IntVar(&c.FPS, "fps", 60, "max frames per second")
	flag.DurationVar(&c.Delay, "delay", 0, "delay before producer starts")
	flag.Parse()
	return c
}

// FrameInterval returns the minimum time between frames (0 = unlimited).
func (c Config) FrameInterval() time.Duration {
	if c.FPS <= 0 {
		return 0
	}
	return time.Second / time.Duration(c.FPS)
}

type Kind uint8

const (
	KText Kind = iota
	KTool
	KResult
	KError
)

// Event is one synthetic agent event: it appends one line and bumps counters.
type Event struct {
	Seq    int
	Kind   Kind
	TS     string
	Text   string
	TokIn  int
	TokOut int
}

var tools = []string{"read_file", "grep", "edit_file", "bash", "list_dir", "web_fetch"}

// Gen deterministically builds event i (1-based).
func Gen(i int) Event {
	ms := i * 7
	ts := fmt.Sprintf("%02d:%02d:%02d.%03d", ms/3600000, (ms/60000)%60, (ms/1000)%60, ms%1000)
	tool := tools[i%len(tools)]
	var k Kind
	var text string
	switch {
	case i%97 == 0:
		k = KError
		text = fmt.Sprintf("✗ %s failed: exit status 1 (pkg/mod%03d/handler_%d.go:%d)", tool, i%1000, i, i%400)
	case i%10 == 0:
		k = KText
		text = fmt.Sprintf("assistant: turn %d — looking at the failing test in internal/svc%02d before editing", i/10, i%50)
	case i%2 == 1:
		k = KTool
		text = fmt.Sprintf("▶ %s path=internal/pkg%02d/file_%05d.go lines=%d-%d", tool, i%50, i, i%300, i%300+40)
	default:
		k = KResult
		text = fmt.Sprintf("✓ %s ok · %d bytes · %dms", tool, 100+(i*37)%9000, 1+(i*13)%250)
	}
	return Event{Seq: i, Kind: k, TS: ts, Text: text, TokIn: 100 + i%50, TokOut: 20 + i%7}
}

// Tag returns the short kind label shown between timestamp and text.
func (k Kind) Tag() string {
	switch k {
	case KTool:
		return "tool "
	case KResult:
		return "done "
	case KError:
		return "error"
	default:
		return "text "
	}
}

// Produce emits cfg.N events through emit, paced at cfg.Rate. It blocks.
// emit may block (natural back-pressure from the UI loop).
func Produce(cfg Config, emit func(Event)) {
	if cfg.Delay > 0 {
		time.Sleep(cfg.Delay)
	}
	ProdStart.Store(time.Now().UnixNano())
	start := time.Now()
	var interval time.Duration
	if cfg.Rate > 0 {
		interval = time.Duration(float64(time.Second) / cfg.Rate)
	}
	for i := 1; i <= cfg.N; i++ {
		if interval > 0 {
			due := start.Add(time.Duration(i-1) * interval)
			if d := time.Until(due); d > 0 {
				time.Sleep(d)
			}
		}
		emit(Gen(i))
	}
	ProdEnd.Store(time.Now().UnixNano())
}

var (
	ProdStart atomic.Int64
	ProdEnd   atomic.Int64
)

// Counters are the numbers shown in the status line.
type Counters struct {
	Count, N      int
	TokIn, TokOut int
}

func (c *Counters) Apply(e Event) {
	c.Count++
	c.TokIn += e.TokIn
	c.TokOut += e.TokOut
}

func (c Counters) Done() bool { return c.Count >= c.N }

// Status returns the status-line text. It ends with BUSY/DONE and the "@@"
// sentinel; the harness uses "@@" to detect the first full frame and "DONE"
// to detect that the final frame hit the terminal. BUSY and DONE share no
// character position so a diff renderer must emit all four bytes.
func (c Counters) Status() string {
	st := "BUSY"
	if c.Done() {
		st = "DONE"
	}
	var b strings.Builder
	b.Grow(96)
	b.WriteString(" uagent-spike │ model sonnet │ effort high │ events ")
	b.WriteString(strconv.Itoa(c.Count))
	b.WriteByte('/')
	b.WriteString(strconv.Itoa(c.N))
	b.WriteString(" │ tok in ")
	b.WriteString(strconv.Itoa(c.TokIn))
	b.WriteString(" out ")
	b.WriteString(strconv.Itoa(c.TokOut))
	b.WriteString(" │ ")
	b.WriteString(st)
	b.WriteString(" @@")
	return b.String()
}

// Stats is written as JSON on exit.
type Stats struct {
	Framework   string `json:"framework"`
	N           int    `json:"n"`
	Count       int    `json:"count"`
	Lines       int    `json:"lines"`
	Frames      int64  `json:"frames"`
	ProdStartNs int64  `json:"prod_start_ns"`
	ProdEndNs   int64  `json:"prod_end_ns"`
	DoneFrameNs int64  `json:"done_frame_ns"`
	HeapAllocGC uint64 `json:"heap_alloc_after_gc"`
	HeapSys     uint64 `json:"heap_sys"`
	TotalAlloc  uint64 `json:"total_alloc"`
	NumGC       uint32 `json:"num_gc"`
	Mallocs     uint64 `json:"mallocs"`
}

// Frames counts frames drawn (incremented by the spikes / renderer hooks).
var Frames atomic.Int64

// DoneFrame records when the first frame containing the final count was drawn.
var DoneFrame atomic.Int64

func MarkDoneFrame() {
	DoneFrame.CompareAndSwap(0, time.Now().UnixNano())
}

// WriteStats writes the stats file. keep must reference the retained
// transcript so it is still reachable during the post-GC heap measurement.
func WriteStats(cfg Config, framework string, count, lines int, keep any) {
	if cfg.Stats == "" {
		return
	}
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	total, mallocs, ngc := ms.TotalAlloc, ms.Mallocs, ms.NumGC
	runtime.GC()
	runtime.ReadMemStats(&ms)
	runtime.KeepAlive(keep)
	s := Stats{
		Framework: framework, N: cfg.N, Count: count, Lines: lines,
		Frames:      Frames.Load(),
		ProdStartNs: ProdStart.Load(), ProdEndNs: ProdEnd.Load(), DoneFrameNs: DoneFrame.Load(),
		HeapAllocGC: ms.HeapAlloc, HeapSys: ms.HeapSys, TotalAlloc: total, NumGC: ngc, Mallocs: mallocs,
	}
	b, _ := json.Marshal(s)
	_ = os.WriteFile(cfg.Stats, b, 0o644)
}

// Line is the retained transcript entry for the raw (non-BT, non-tview)
// spikes. Strings share the backing of the event; no pre-rendered ANSI.
type Line struct {
	Kind Kind
	TS   string
	Text string
}

// Input is a minimal multi-line input model (rune buffer + cursor) used by
// the raw spikes. Enter submits, Alt/Shift-Enter would insert a newline in a
// real app; here Ctrl-J inserts a newline.
type Input struct {
	Buf    []rune
	Cursor int
}

func (in *Input) Insert(s string) {
	rs := []rune(s)
	in.Buf = append(in.Buf[:in.Cursor], append(rs, in.Buf[in.Cursor:]...)...)
	in.Cursor += len(rs)
}

func (in *Input) Backspace() {
	if in.Cursor > 0 {
		in.Buf = append(in.Buf[:in.Cursor-1], in.Buf[in.Cursor:]...)
		in.Cursor--
	}
}

func (in *Input) Left() {
	if in.Cursor > 0 {
		in.Cursor--
	}
}

func (in *Input) Right() {
	if in.Cursor < len(in.Buf) {
		in.Cursor++
	}
}

func (in *Input) Submit() string {
	s := string(in.Buf)
	in.Buf = in.Buf[:0]
	in.Cursor = 0
	return s
}

// Wrap splits the buffer into rows of at most width runes (hard newlines
// respected) and returns the rows plus the cursor row/col. Keeps only the
// last `rows` rows that contain the cursor. Width is rune-count based, which
// is fine for this ASCII-heavy benchmark.
func (in *Input) Wrap(width, rows int) (lines []string, cr, cc int) {
	if width < 1 {
		width = 1
	}
	var cur []rune
	cr, cc = -1, 0
	for i := 0; i <= len(in.Buf); i++ {
		if i == in.Cursor {
			cr, cc = len(lines), len(cur)
		}
		if i == len(in.Buf) {
			break
		}
		r := in.Buf[i]
		if r == '\n' {
			lines = append(lines, string(cur))
			cur = cur[:0]
			continue
		}
		cur = append(cur, r)
		if len(cur) >= width {
			lines = append(lines, string(cur))
			cur = cur[:0]
		}
	}
	lines = append(lines, string(cur))
	if cr >= len(lines) {
		cr = len(lines) - 1
	}
	if len(lines) > rows {
		off := len(lines) - rows
		if cr-off < 0 {
			off = cr
		}
		lines = lines[off : off+rows]
		cr -= off
	}
	return lines, cr, cc
}

// Throttle coalesces redraw requests to at most one per interval.
// Call Request() when state changes; it returns a channel that fires when a
// frame should be drawn (nil if no frame is pending).
type Throttle struct {
	Interval time.Duration
	last     time.Time
	timer    *time.Timer
	pending  bool
}

// Request marks the screen dirty. If drawNow is returned true the caller
// should draw immediately; otherwise it should wait on C().
func (t *Throttle) Request() (drawNow bool) {
	if t.pending {
		return false
	}
	now := time.Now()
	if t.Interval == 0 || now.Sub(t.last) >= t.Interval {
		t.last = now
		return true
	}
	t.pending = true
	d := t.Interval - now.Sub(t.last)
	if t.timer == nil {
		t.timer = time.NewTimer(d)
	} else {
		t.timer.Reset(d)
	}
	return false
}

// C returns the timer channel (nil when nothing is pending: blocks forever in select).
func (t *Throttle) C() <-chan time.Time {
	if !t.pending || t.timer == nil {
		return nil
	}
	return t.timer.C
}

// Fired must be called when C() fired, before drawing.
func (t *Throttle) Fired() {
	t.pending = false
	t.last = time.Now()
}

// Placeholder is shown in the empty input box by every spike.
const Placeholder = "steer the agent… (/model /effort /fast)"
