package invoice

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"example.com/billing/internal/customer"
	"example.com/billing/internal/plan"
)

func utcDate(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

var pro = plan.Plan{ID: "pro", Name: "Pro", Monthly: 3100}

func TestBuildFullMonth(t *testing.T) {
	c := customer.Customer{ID: "c1", Location: time.UTC, Start: utcDate(2026, 1, 5)}
	inv, ok := Build(c, pro, 2026, time.March)
	if !ok {
		t.Fatal("not billed")
	}
	if inv.Total != 3100 || inv.Lines[0].Days != 31 {
		t.Fatalf("got %+v", inv)
	}
	if inv.Number != "INV-2026-03-c1" || !inv.Due.Equal(utcDate(2026, 4, 15)) {
		t.Fatalf("number %s due %v", inv.Number, inv.Due)
	}
}

func TestBuildProratesStart(t *testing.T) {
	c := customer.Customer{ID: "c2", Location: time.UTC, Start: utcDate(2026, 3, 15)}
	inv, _ := Build(c, pro, 2026, time.March)
	if inv.Lines[0].Days != 17 || inv.Total != 1700 {
		t.Fatalf("got %+v", inv.Lines[0])
	}
}

func TestBuildProratesEnd(t *testing.T) {
	c := customer.Customer{ID: "c3", Location: time.UTC, Start: utcDate(2026, 1, 1), End: utcDate(2026, 4, 11)}
	inv, _ := Build(c, plan.Plan{Name: "Team", Monthly: 12000}, 2026, time.April)
	if inv.Lines[0].Days != 10 || inv.Total != 4000 {
		t.Fatalf("got %+v", inv.Lines[0])
	}
	if _, ok := Build(c, pro, 2026, time.May); ok {
		t.Fatal("billed after the end")
	}
}

func TestWriteText(t *testing.T) {
	c := customer.Customer{ID: "c2", Location: time.UTC, Start: utcDate(2026, 3, 15)}
	inv, _ := Build(c, pro, 2026, time.March)
	var buf bytes.Buffer
	if err := WriteText(&buf, inv); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"INV-2026-03-c2", "17 of 31 days", "17.00", "due 2026-04-15"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, buf.String())
		}
	}
}
