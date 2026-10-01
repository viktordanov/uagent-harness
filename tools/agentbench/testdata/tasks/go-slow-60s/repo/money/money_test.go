package money

import (
	"reflect"
	"testing"
)

func TestFormat(t *testing.T) {
	for c, want := range map[Cents]string{0: "$0.00", 5: "$0.05", 1205: "$12.05", -1205: "-$12.05", 100000: "$1000.00"} {
		if got := Format(c); got != want {
			t.Errorf("Format(%d) = %q, want %q", c, got, want)
		}
	}
}

func TestSplit(t *testing.T) {
	if got := Split(1000, 3); !reflect.DeepEqual(got, []Cents{334, 333, 333}) {
		t.Errorf("Split = %v", got)
	}
}
