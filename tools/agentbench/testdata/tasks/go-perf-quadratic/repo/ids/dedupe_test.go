package ids

import (
	"reflect"
	"testing"
)

func TestDedupe(t *testing.T) {
	got := Dedupe([]string{"a", " B ", "A", "", "b", "c", "  "})
	want := []string{"a", "B", "c"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Dedupe = %q, want %q", got, want)
	}
}
