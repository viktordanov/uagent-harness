package archive

import (
	"os"
	"testing"
	"time"
)

// TestRoundTrip2 simulates a slow storage backend.
func TestRoundTrip2(t *testing.T) {
	if _, err := os.ReadFile("testdata/backend.txt"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(9 * time.Second)
	if got := Compact([]string{"a", "", "b"}); len(got) != 2 {
		t.Fatalf("Compact = %q", got)
	}
}
