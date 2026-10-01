package size

import "testing"

func TestParseSizeSimple(t *testing.T) {
	if n, err := ParseSize("10 KiB"); err != nil || n != 10240 {
		t.Fatalf("ParseSize = %d, %v", n, err)
	}
}
