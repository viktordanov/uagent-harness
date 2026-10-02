package format

import (
	"strings"
	"testing"
	"time"

	"example.com/logq/internal/entry"
)

func TestText(t *testing.T) {
	p, err := New("text")
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	e := entry.Entry{Time: time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC), Level: entry.Warn, Msg: "slow", Fields: map[string]any{"ms": 900, "db": "main"}}
	if err := p.Print(&b, e); err != nil {
		t.Fatal(err)
	}
	if want := "2026-03-01T10:00:00Z WARN  slow db=main ms=900\n"; b.String() != want {
		t.Errorf("got %q, want %q", b.String(), want)
	}
}
