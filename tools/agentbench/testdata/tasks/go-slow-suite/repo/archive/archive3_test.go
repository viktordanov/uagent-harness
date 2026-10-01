package archive

import (
	"os"
	"testing"
	"time"
)

// TestRoundTrip3 simulates a slow storage backend.
func TestRoundTrip3(t *testing.T) {
	if _, err := os.ReadFile("testdata/backend.txt"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(9 * time.Second)
	if got := Compact([]string{"a", "", "b"}); len(got) != 2 {
		t.Fatalf("Compact = %q", got)
	}
}
