package stats

import "testing"

func TestCounter(t *testing.T) {
	c := New()
	c.Inc("a")
	c.Add("b", 3)
	if c.Get("a") != 1 || c.Get("b") != 3 || c.Total() != 4 {
		t.Fatalf("counts %v total %d", c.Snapshot(), c.Total())
	}
	if n := c.Names(); len(n) != 2 || n[0] != "a" {
		t.Fatalf("Names = %v", n)
	}
	c.Reset()
	if c.Total() != 0 || c.Get("a") != 0 {
		t.Fatal("Reset")
	}
}
