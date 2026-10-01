package billing

import (
	"io"
	"testing"

	"example.com/shop/logx"
)

func TestCharge(t *testing.T) {
	logx.SetOutput(io.Discard)
	if err := Charge("4111111111110000", 100); err != ErrDeclined {
		t.Fatalf("want ErrDeclined, got %v", err)
	}
	if err := Charge("4111111111111111", 100); err != nil {
		t.Fatal(err)
	}
}
