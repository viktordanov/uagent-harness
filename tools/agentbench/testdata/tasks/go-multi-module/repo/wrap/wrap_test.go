package wrap

import (
	"reflect"
	"testing"
)

func TestLines(t *testing.T) {
	got := Lines("the quick brown fox jumps over the lazy dog", 15)
	want := []string{"the quick brown", "fox jumps over", "the lazy dog"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Lines = %q, want %q", got, want)
	}
	if got := Lines("", 10); len(got) != 0 {
		t.Fatalf("Lines(\"\") = %q", got)
	}
	if got := Lines("abcdefghijkl xy", 5); !reflect.DeepEqual(got, []string{"abcdefghijkl", "xy"}) {
		t.Fatalf("long word: %q", got)
	}
}
