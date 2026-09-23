// Command harness runs a spike binary inside a real pseudo-terminal, plays
// the part of a minimal modern terminal (answers DA1, CPR, DECRQM and OSC
// 10/11 colour queries so apps that probe the terminal are not stalled),
// counts every byte the app writes, timestamps sentinels in the output
// stream, samples RSS/CPU with ps, and collects rusage at exit.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
)

type result struct {
	Bin         string  `json:"bin"`
	Args        string  `json:"args"`
	FirstByteMs float64 `json:"first_byte_ms"`
	FirstFullMs float64 `json:"first_full_ms"`
	DoneSeenMs  float64 `json:"done_seen_ms"`
	ExitMs      float64 `json:"exit_ms"`
	Bytes       int64   `json:"bytes"`
	UserMs      float64 `json:"user_ms"`
	SysMs       float64 `json:"sys_ms"`
	MaxRSSKB    int64   `json:"maxrss_kb"`
	// ps samples
	IdleRSSKB     int64           `json:"idle_rss_kb,omitempty"`
	IdleCPUms     float64         `json:"idle_cpu_ms"` // cpu time consumed during idle window (proc_pid_rusage)
	IdleFootKB    int64           `json:"idle_footprint_kb,omitempty"`
	AfterDoneFoot int64           `json:"after_done_footprint_kb,omitempty"`
	IdleWindowS   float64         `json:"idle_window_s,omitempty"`
	AfterDoneRSS  int64           `json:"after_done_rss_kb,omitempty"`
	Queries       int             `json:"queries_answered"`
	DoneSeenUnix  int64           `json:"done_seen_unix_ns"`
	Stats         json.RawMessage `json:"stats,omitempty"`
	TimedOut      bool            `json:"timed_out,omitempty"`
}

var queryRe = regexp.MustCompile(`\x1b\[(0?c|6n|\?6n|\?(\d+)\$p)|\x1b\]1([01]);\?(?:\x07|\x1b\\)`)

func main() {
	bin := flag.String("bin", "", "binary to run")
	args := flag.String("args", "", "arguments (space separated)")
	cols := flag.Int("cols", 120, "")
	rows := flag.Int("rows", 40, "")
	respond := flag.Bool("respond", true, "answer terminal queries like a modern terminal")
	repeat := flag.Int("repeat", 1, "repeat count (cold-start mode)")
	cold := flag.Bool("cold", false, "cold-start mode: kill as soon as first full frame is seen")
	idle := flag.Duration("idle", 0, "idle mode: after first frame wait 1s, then measure CPU over this window, then quit")
	timeout := flag.Duration("timeout", 180*time.Second, "")
	stats := flag.String("stats", "", "stats file the app writes (passed via -stats)")
	dump := flag.String("dump", "", "write raw pty output here")
	flag.Parse()

	var all []result
	for i := 0; i < *repeat; i++ {
		r := runOnce(*bin, *args, *cols, *rows, *respond, *cold, *idle, *timeout, *stats, *dump)
		all = append(all, r)
		b, _ := json.Marshal(r)
		fmt.Println(string(b))
	}
	if *repeat > 1 {
		med := func(get func(result) float64) float64 {
			v := make([]float64, 0, len(all))
			for _, r := range all {
				v = append(v, get(r))
			}
			sort.Float64s(v)
			n := len(v)
			if n%2 == 1 {
				return v[n/2]
			}
			return (v[n/2-1] + v[n/2]) / 2
		}
		fmt.Printf("SUMMARY bin=%s runs=%d median_first_byte_ms=%.2f median_first_full_ms=%.2f min_first_full_ms=%.2f max_first_full_ms=%.2f\n",
			*bin, len(all), med(func(r result) float64 { return r.FirstByteMs }), med(func(r result) float64 { return r.FirstFullMs }),
			minOf(all), maxOf(all))
	}
}

func minOf(a []result) float64 {
	m := a[0].FirstFullMs
	for _, r := range a {
		m = min(m, r.FirstFullMs)
	}
	return m
}

func maxOf(a []result) float64 {
	m := a[0].FirstFullMs
	for _, r := range a {
		m = max(m, r.FirstFullMs)
	}
	return m
}

func psSample(pid int) (rssKB int64, cpu time.Duration) {
	out, err := exec.Command("ps", "-o", "rss=,time=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return 0, 0
	}
	f := strings.Fields(string(out))
	if len(f) < 2 {
		return 0, 0
	}
	rssKB, _ = strconv.ParseInt(f[0], 10, 64)
	// time format: [[dd-]hh:]mm:ss.cc
	t := f[1]
	var total float64
	parts := strings.Split(t, ":")
	mult := 1.0
	for i := len(parts) - 1; i >= 0; i-- {
		v, _ := strconv.ParseFloat(parts[i], 64)
		total += v * mult
		mult *= 60
	}
	return rssKB, time.Duration(total * float64(time.Second))
}

func runOnce(bin, args string, cols, rows int, respond, cold bool, idle, timeout time.Duration, statsPath, dump string) result {
	r := result{Bin: bin, Args: args}
	if statsPath != "" {
		_ = os.Remove(statsPath)
	}
	cmd := exec.Command(bin, strings.Fields(args)...)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color", "COLORTERM=truecolor")
	// drop env that may change behaviour between runs
	t0 := time.Now()
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)})
	if err != nil {
		panic(err)
	}
	var (
		mu        sync.Mutex
		firstByte time.Duration = -1
		firstFull time.Duration = -1
		doneSeen  time.Duration = -1
		total     int64
		queries   int
	)
	fullC := make(chan struct{})
	doneC := make(chan struct{})
	var dumpBuf bytes.Buffer
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		buf := make([]byte, 256*1024)
		var carry []byte
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				now := time.Since(t0)
				mu.Lock()
				if firstByte < 0 {
					firstByte = now
				}
				total += int64(n)
				mu.Unlock()
				if dump != "" {
					dumpBuf.Write(buf[:n])
				}
				win := append(carry, buf[:n]...)
				if respond {
					last := 0
					for _, m := range queryRe.FindAllSubmatchIndex(win, -1) {
						var reply string
						q := string(win[m[0]:m[1]])
						switch {
						case strings.HasSuffix(q, "c"):
							reply = "\x1b[?62;22;52c"
						case q == "\x1b[6n":
							reply = "\x1b[1;1R"
						case q == "\x1b[?6n":
							reply = "\x1b[?1;1;1R"
						case strings.HasSuffix(q, "$p"):
							mode := string(win[m[4]:m[5]])
							st := "0"
							if mode == "2026" {
								st = "2"
							}
							reply = "\x1b[?" + mode + ";" + st + "$y"
						default: // OSC 10/11
							which := string(win[m[6]:m[7]])
							if which == "0" {
								reply = "\x1b]10;rgb:dddd/dddd/dddd\x1b\\"
							} else {
								reply = "\x1b]11;rgb:1111/1111/1111\x1b\\"
							}
						}
						_, _ = ptmx.Write([]byte(reply))
						queries++
						last = m[1]
					}
					win = win[last:]
				}
				if firstFull < 0 && bytes.Contains(win, []byte("@@")) {
					firstFull = time.Since(t0)
					close(fullC)
				}
				if doneSeen < 0 && bytes.Contains(win, []byte("DONE")) {
					doneSeen = time.Since(t0)
					r.DoneSeenUnix = time.Now().UnixNano()
					close(doneC)
				}
				if len(win) > 64 {
					win = win[len(win)-64:]
				}
				carry = append(carry[:0:0], win...)
			}
			if err != nil {
				return
			}
		}
	}()

	waitC := make(chan error, 1)
	go func() { waitC <- cmd.Wait() }()

	deadline := time.After(timeout)
	pid := cmd.Process.Pid
	switch {
	case cold:
		select {
		case <-fullC:
			_ = cmd.Process.Kill()
		case <-deadline:
			r.TimedOut = true
			_ = cmd.Process.Kill()
		}
	case idle > 0:
		select {
		case <-fullC:
		case <-deadline:
			r.TimedOut = true
		}
		time.Sleep(time.Second)
		c1, _, _, _ := pidUsage(pid)
		time.Sleep(idle)
		c2, rss2, foot, _ := pidUsage(pid)
		r.IdleRSSKB = rss2
		r.IdleFootKB = foot
		r.IdleCPUms = float64(c2-c1) / 1e6
		r.IdleWindowS = idle.Seconds()
		_, _ = ptmx.Write([]byte{3}) // Ctrl-C
		select {
		case <-waitC:
			waitC <- nil
		case <-time.After(2 * time.Second):
			_ = cmd.Process.Kill()
		}
	default:
		select {
		case <-doneC:
			time.Sleep(300 * time.Millisecond)
			_, r.AfterDoneRSS, r.AfterDoneFoot, _ = pidUsage(pid)
		case <-waitC:
			waitC <- nil
		case <-deadline:
			r.TimedOut = true
			_ = cmd.Process.Kill()
		}
	}
	select {
	case <-waitC:
	case <-time.After(20 * time.Second):
		_ = cmd.Process.Kill()
		<-waitC
		r.TimedOut = true
	}
	r.ExitMs = ms(time.Since(t0))
	// drain remaining output
	go func() { time.Sleep(300 * time.Millisecond); ptmx.Close() }()
	<-readerDone
	_, _ = io.Copy(io.Discard, ptmx)

	mu.Lock()
	r.FirstByteMs = ms(firstByte)
	r.FirstFullMs = ms(firstFull)
	r.DoneSeenMs = ms(doneSeen)
	r.Bytes = total
	r.Queries = queries
	mu.Unlock()
	if ru, ok := cmd.ProcessState.SysUsage().(*syscall.Rusage); ok {
		r.UserMs = float64(time.Duration(ru.Utime.Nano())) / 1e6
		r.SysMs = float64(time.Duration(ru.Stime.Nano())) / 1e6
		r.MaxRSSKB = ru.Maxrss / 1024 // bytes on darwin
	}
	if statsPath != "" {
		if b, err := os.ReadFile(statsPath); err == nil {
			r.Stats = b
		}
	}
	if dump != "" {
		_ = os.WriteFile(dump, dumpBuf.Bytes(), 0o644)
	}
	return r
}

func ms(d time.Duration) float64 {
	if d < 0 {
		return -1
	}
	return float64(d.Microseconds()) / 1000
}
