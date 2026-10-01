package invoice

import "testing"

func TestPriceNoCode(t *testing.T) {
	got := Price(Order{Lines: []Line{{"A", 2, 1999}, {"B", 1, 500}}, Region: "NY"})
	want := Totals{Subtotal: 4498, Discounted: 4498, Tax: 180, Total: 4678}
	if got != want {
		t.Fatalf("Price = %+v, want %+v", got, want)
	}
}

func TestPriceWelcome(t *testing.T) {
	got := Price(Order{Lines: []Line{{"A", 1, 2000}}, Code: "welcome10", Region: "OR"})
	if got.Total != 1800 {
		t.Fatalf("Total = %v, want $18.00", got.Total)
	}
}

func TestCentsString(t *testing.T) {
	if s := Cents(-1205).String(); s != "-$12.05" {
		t.Fatalf("String = %q", s)
	}
}
