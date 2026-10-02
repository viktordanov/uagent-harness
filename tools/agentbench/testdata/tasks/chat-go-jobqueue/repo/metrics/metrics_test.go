package metrics

import (
	"strings"
	"sync"
	"testing"
	"time"
)

func TestNilCountersAreSafe(t *testing.T) {
	var c *Counters
	c.Claimed("echo")
	c.Succeeded("echo", time.Second)
	c.Retried("echo")
	c.Failed("echo")
	if s := c.Snapshot(); s.Claimed != 0 || len(s.Kinds) != 0 {
		t.Fatalf("nil snapshot: %+v", s)
	}
}

func TestTotalsAndKinds(t *testing.T) {
	c := New()
	c.Claimed("echo")
	c.Succeeded("echo", 2*time.Millisecond)
	c.Claimed("fail")
	c.Retried("fail")
	c.Claimed("fail")
	c.Failed("fail")
	s := c.Snapshot()
	if s.Claimed != 3 || s.Succeeded != 1 || s.Retried != 1 || s.Failed != 1 {
		t.Fatalf("totals: %+v", s)
	}
	if got := s.Summary(); got != "3 claimed, 1 succeeded, 1 retried, 1 failed" {
		t.Fatalf("Summary() = %q", got)
	}
	if names := s.KindNames(); len(names) != 2 || names[0] != "echo" || names[1] != "fail" {
		t.Fatalf("KindNames() = %v", names)
	}
}

func TestWriteTable(t *testing.T) {
	c := New()
	c.Claimed("echo")
	c.Succeeded("echo", 4*time.Millisecond)
	c.Claimed("echo")
	c.Succeeded("echo", 2*time.Millisecond)
	var b strings.Builder
	if err := c.Snapshot().WriteTable(&b); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(b.String()), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "KIND") {
		t.Fatalf("table:\n%s", b.String())
	}
	if f := strings.Fields(lines[1]); len(f) != 6 || f[0] != "echo" || f[2] != "2" || f[5] != "3ms" {
		t.Fatalf("row %q", lines[1])
	}
}

func TestConcurrentCounting(t *testing.T) {
	c := New()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				c.Claimed("echo")
				c.Succeeded("echo", time.Microsecond)
			}
		}()
	}
	wg.Wait()
	if s := c.Snapshot(); s.Claimed != 800 || s.Succeeded != 800 {
		t.Fatalf("lost counts: %+v", s)
	}
}
