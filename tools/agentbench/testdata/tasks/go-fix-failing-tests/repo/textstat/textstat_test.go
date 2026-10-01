package textstat

import (
	"reflect"
	"testing"
)

func TestWords(t *testing.T) {
	got := Words(`The cat, the "Dog" and THE bird.`)
	want := []string{"the", "cat", "the", "dog", "and", "the", "bird"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Words = %q, want %q", got, want)
	}
}

func TestTop(t *testing.T) {
	got := Top([]string{"b", "a", "c", "a", "b", "d"}, 3)
	want := []Count{{"a", 2}, {"b", 2}, {"c", 1}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Top = %v, want %v", got, want)
	}
}

func TestTruncate(t *testing.T) {
	for _, c := range []struct {
		in   string
		max  int
		want string
	}{
		{"hello", 10, "hello"},
		{"hello world", 6, "hello…"},
		{"héllo wörld", 6, "héllo…"},
		{"日本語のテキスト", 4, "日本語…"},
		{"äöü", 3, "äöü"},
	} {
		if got := Truncate(c.in, c.max); got != c.want {
			t.Errorf("Truncate(%q, %d) = %q, want %q", c.in, c.max, got, c.want)
		}
	}
}
