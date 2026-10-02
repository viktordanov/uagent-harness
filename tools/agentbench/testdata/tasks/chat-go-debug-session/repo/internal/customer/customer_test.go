package customer

import (
	"strings"
	"testing"
	"time"

	"example.com/billing/internal/period"
)

const sample = `[
  {"id": "c1", "name": "Ada", "plan": "pro", "start": "2026-01-10"},
  {"id": "c2", "name": "Bo", "plan": "team", "timezone": "Asia/Tokyo", "start": "2026-02-01", "end": "2026-03-20"}
]`

func TestLoad(t *testing.T) {
	cs, err := Load(strings.NewReader(sample))
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 2 || cs[0].Location != time.UTC || cs[1].Location.String() != "Asia/Tokyo" {
		t.Fatalf("loaded %+v", cs)
	}
	if !cs[1].End.Equal(time.Date(2026, 3, 20, 0, 0, 0, 0, cs[1].Location)) {
		t.Fatalf("End = %v", cs[1].End)
	}
}

func TestLoadErrors(t *testing.T) {
	for _, in := range []string{
		`{}`,
		`[{"name": "no id", "start": "2026-01-01"}]`,
		`[{"id": "x", "timezone": "Mars/Base", "start": "2026-01-01"}]`,
		`[{"id": "x", "start": "01/02/2026"}]`,
		`[{"id": "x", "start": "2026-02-01", "end": "2026-01-01"}]`,
	} {
		if _, err := Load(strings.NewReader(in)); err == nil {
			t.Errorf("%s: no error", in)
		}
	}
}

func TestActive(t *testing.T) {
	cs, _ := Load(strings.NewReader(sample))
	march := period.Month(2026, time.March, time.UTC)
	a, ok := cs[0].Active(march)
	if !ok || a != march {
		t.Fatalf("c1 active %v %v", a, ok)
	}
	april := period.Month(2026, time.April, cs[1].Location)
	if _, ok := cs[1].Active(april); ok {
		t.Fatal("c2 active in April after it ended")
	}
}
