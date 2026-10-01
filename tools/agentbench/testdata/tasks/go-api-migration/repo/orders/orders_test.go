package orders

import (
	"io"
	"testing"

	"example.com/shop/logx"
)

func TestPlace(t *testing.T) {
	logx.SetOutput(io.Discard)
	if _, err := Place("ann", 0, 0); err != ErrEmpty {
		t.Fatalf("want ErrEmpty, got %v", err)
	}
	o, err := Place("ann", 2, 2500)
	if err != nil || o.Items != 2 {
		t.Fatalf("Place = %+v %v", o, err)
	}
	if Audit([]Order{o, {ID: 9, Cents: 200000}}) != 202500 {
		t.Fatal("Audit total")
	}
}
