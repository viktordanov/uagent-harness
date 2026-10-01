package perf

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"runtime/metrics"
	"runtime/pprof"
	"slices"
	"syscall"
	"time"
)

// Sample is what one measured block cost the process.
type Sample struct {
	Wall time.Duration `json:"-"`
	// CPU is the process's user and system time; ChildCPU the commands'
	// it waited for (shell tool calls).
	CPU      time.Duration `json:"-"`
	ChildCPU time.Duration `json:"-"`
	// AllocBytes and Allocs are what the Go heap allocated; PeakHeap is
	// the most live heap seen (sampled every few milliseconds).
	AllocBytes uint64 `json:"alloc_bytes"`
	Allocs     uint64 `json:"allocs"`
	PeakHeap   uint64 `json:"peak_heap_bytes"`
	// Goroutines before the block and after it (and its cleanup) settled;
	// the report's goroutines_left is their difference, less one server
	// goroutine per connection still open.
	GoroutinesBefore int `json:"goroutines_before"`
	GoroutinesAfter  int `json:"goroutines_after"`
	// ConnsAfter are the connections to the fake model still open after
	// the block, idle ones included.
	ConnsAfter int `json:"conns_after"`
	// DiskWritten is what the process wrote to disk (macOS: the kernel's
	// count; Linux: /proc/self/io write_bytes; elsewhere 0).
	DiskWritten uint64 `json:"disk_written_bytes"`
	// StateGrowth is how much the state directory grew.
	StateGrowth int64 `json:"state_growth_bytes"`
	// Wakeups are the process's wakeups (macOS: idle and interrupt
	// wakeups; elsewhere voluntary context switches).
	Wakeups uint64 `json:"wakeups"`
	// Extra are the scenario's own measurements, by name; names ending in
	// _ms are milliseconds.
	Extra map[string]float64 `json:"extra,omitempty"`
}

// counters are the process counters a sample subtracts.
type counters struct {
	at            time.Time
	cpu, childCPU time.Duration
	alloc, allocs uint64
	disk, wakeups uint64
	goroutines    int
}

func readCounters() counters {
	c := counters{at: time.Now(), goroutines: runtime.NumGoroutine()}
	var self, children syscall.Rusage
	if syscall.Getrusage(syscall.RUSAGE_SELF, &self) == nil {
		c.cpu = tv(self.Utime) + tv(self.Stime)
	}
	if syscall.Getrusage(syscall.RUSAGE_CHILDREN, &children) == nil {
		c.childCPU = tv(children.Utime) + tv(children.Stime)
	}
	samples := []metrics.Sample{{Name: "/gc/heap/allocs:bytes"}, {Name: "/gc/heap/allocs:objects"}}
	metrics.Read(samples)
	c.alloc, c.allocs = samples[0].Value.Uint64(), samples[1].Value.Uint64()
	c.disk, c.wakeups = diskAndWakeups(self)

	return c
}

func tv(t syscall.Timeval) time.Duration {
	return time.Duration(t.Sec)*time.Second + time.Duration(t.Usec)*time.Microsecond //nolint:unconvert // int32 on some platforms
}

// heapSampler records the most live heap while it runs.
type heapSampler struct {
	stop chan struct{}
	done chan uint64
}

const heapSampleEvery = 2 * time.Millisecond

func startHeapSampler() *heapSampler {
	h := &heapSampler{stop: make(chan struct{}), done: make(chan uint64, 1)}
	go func() {
		s := []metrics.Sample{{Name: "/memory/classes/heap/objects:bytes"}}
		var peak uint64
		t := time.NewTicker(heapSampleEvery)
		defer t.Stop()
		for {
			metrics.Read(s)
			peak = max(peak, s[0].Value.Uint64())
			select {
			case <-h.stop:
				h.done <- peak

				return
			case <-t.C:
			}
		}
	}()

	return h
}

func (h *heapSampler) peak() uint64 {
	close(h.stop)

	return <-h.done
}

// Probe measures blocks of one scenario.
type Probe struct {
	// Name names the profiles; ProfileDir, when set, gets a CPU profile
	// and heap allocation profiles (before and after) of the block.
	Name       string
	ProfileDir string
	CPUProfile bool
	MemProfile bool
	// State is the directory whose growth the sample reports; LLM the
	// fake model whose connections it counts.
	State string
	Conns func() int
	// Goroutines, with ProfileDir, writes the stacks of the goroutines
	// left after the block when there are more than before.
	Goroutines bool
	// Quiet leaves out the heap sampler, whose wakeups an idle
	// measurement would count.
	Quiet bool
}

// Measure runs block and samples its cost. Cleanup runs after it, outside
// the timing, before the goroutines and connections are counted.
func (p Probe) Measure(block func(*Sample) error, cleanup func()) (Sample, error) {
	runtime.GC()
	stateBefore := dirSize(p.State)
	stopProfiles, err := p.startProfiles()
	if err != nil {
		return Sample{}, err
	}
	s := Sample{Extra: map[string]float64{}}
	before := readCounters()
	var heap *heapSampler
	if !p.Quiet {
		heap = startHeapSampler()
	}
	blockErr := block(&s)
	after := readCounters()
	if heap != nil {
		s.PeakHeap = heap.peak()
	}
	if err := stopProfiles(); err != nil && blockErr == nil {
		blockErr = err
	}
	s.Wall = after.at.Sub(before.at)
	s.CPU, s.ChildCPU = after.cpu-before.cpu, after.childCPU-before.childCPU
	s.AllocBytes, s.Allocs = after.alloc-before.alloc, after.allocs-before.allocs
	s.DiskWritten, s.Wakeups = after.disk-before.disk, after.wakeups-before.wakeups
	s.StateGrowth = dirSize(p.State) - stateBefore
	s.GoroutinesBefore = before.goroutines
	if cleanup != nil {
		cleanup()
	}
	s.GoroutinesAfter = settledGoroutines(before.goroutines)
	if p.Goroutines && p.ProfileDir != "" && s.GoroutinesAfter > s.GoroutinesBefore {
		if err := writeGoroutines(filepath.Join(p.ProfileDir, safeName(p.Name)+".goroutines.txt")); err != nil && blockErr == nil {
			blockErr = err
		}
	}
	if p.Conns != nil {
		s.ConnsAfter = p.Conns()
	}

	return s, blockErr
}

// settleFor is how long goroutines get to end after a block.
const settleFor = 500 * time.Millisecond

// settledGoroutines waits up to settleFor for the goroutine count to fall
// to want, and returns it.
func settledGoroutines(want int) int {
	deadline := time.Now().Add(settleFor)
	for {
		n := runtime.NumGoroutine()
		if n <= want || time.Now().After(deadline) {
			return n
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (p Probe) startProfiles() (func() error, error) {
	if p.ProfileDir == "" || (!p.CPUProfile && !p.MemProfile) {
		return func() error { return nil }, nil
	}
	base := filepath.Join(p.ProfileDir, safeName(p.Name))
	if err := os.MkdirAll(p.ProfileDir, 0o700); err != nil {
		return nil, fmt.Errorf("failed to make the profile directory: %w", err)
	}
	var cpu *os.File
	if p.CPUProfile {
		f, err := os.Create(base + ".cpu.pprof")
		if err != nil {
			return nil, fmt.Errorf("failed to create the CPU profile: %w", err)
		}
		if err := pprof.StartCPUProfile(f); err != nil {
			_ = f.Close()

			return nil, fmt.Errorf("failed to start the CPU profile: %w", err)
		}
		cpu = f
	}
	if p.MemProfile {
		if err := writeHeap(base + ".mem-before.pprof"); err != nil {
			return nil, err
		}
	}

	return func() error {
		var err error
		if cpu != nil {
			pprof.StopCPUProfile()
			err = cpu.Close()
		}
		if p.MemProfile {
			if herr := writeHeap(base + ".mem.pprof"); err == nil {
				err = herr
			}
		}

		return err
	}, nil
}

// writeGoroutines writes every goroutine's stack.
func writeGoroutines(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("failed to make the profile directory: %w", err)
	}
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create the goroutine dump: %w", err)
	}
	if err := pprof.Lookup("goroutine").WriteTo(f, 1); err != nil {
		_ = f.Close()

		return fmt.Errorf("failed to write the goroutine dump: %w", err)
	}

	return f.Close() //nolint:wrapcheck // a close error says enough
}

// writeHeap writes the allocation profile; the block's allocations are
// its difference from the one before (go tool pprof -base).
func writeHeap(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create the heap profile: %w", err)
	}
	if err := pprof.Lookup("allocs").WriteTo(f, 0); err != nil {
		_ = f.Close()

		return fmt.Errorf("failed to write the heap profile: %w", err)
	}

	return f.Close() //nolint:wrapcheck // a close error says enough
}

func safeName(name string) string {
	out := []rune(name)
	for i, r := range out {
		if r == '/' || r == ' ' {
			out[i] = '-'
		}
	}

	return string(out)
}

// dirSize is the size of the files under dir (0 when dir is "").
func dirSize(dir string) int64 {
	if dir == "" {
		return 0
	}
	var n int64
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() {
			if info, err := d.Info(); err == nil {
				n += info.Size()
			}
		}

		return nil
	})

	return n
}

// percentiles are the 50th and 95th percentile and the maximum of ds.
func percentiles(ds []time.Duration) (p50, p95, top time.Duration) {
	if len(ds) == 0 {
		return 0, 0, 0
	}
	sorted := append([]time.Duration(nil), ds...)
	slices.Sort(sorted)

	return sorted[len(sorted)/2], sorted[min(len(sorted)-1, len(sorted)*95/100)], sorted[len(sorted)-1]
}

// ms is d in milliseconds.
func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }
