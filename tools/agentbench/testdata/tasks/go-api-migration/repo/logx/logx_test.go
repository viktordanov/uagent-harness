package logx

import (
	"bytes"
	"testing"
)

func TestInfo(t *testing.T) {
	var b bytes.Buffer
	SetOutput(&b)
	Info("order placed", "id", 7, "total", "12.50")
	if got := b.String(); got != "level=info msg=\"order placed\" id=7 total=12.50\n" {
		t.Fatalf("got %q", got)
	}
}
