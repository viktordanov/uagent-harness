package shipping

import (
	"io"
	"testing"

	"example.com/shop/logx"
)

func TestBook(t *testing.T) {
	logx.SetOutput(io.Discard)
	tn, err := Book(42, "DE", 500)
	if err != nil || tn != "DE-000042" {
		t.Fatalf("Book = %q %v", tn, err)
	}
	if _, err := Book(1, "DE", 40000); err == nil {
		t.Fatal("want an error for a heavy parcel")
	}
}
