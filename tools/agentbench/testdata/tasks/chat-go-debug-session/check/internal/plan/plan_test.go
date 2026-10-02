package plan

import (
	"reflect"
	"testing"
)

func TestLookup(t *testing.T) {
	p, err := Default.Lookup("pro")
	if err != nil || p.Monthly != 3100 {
		t.Fatalf("Lookup(pro) = %+v, %v", p, err)
	}
	if _, err := Default.Lookup("gold"); err == nil {
		t.Fatal("Lookup(gold) did not fail")
	}
}

func TestIDs(t *testing.T) {
	if got := Default.IDs(); !reflect.DeepEqual(got, []string{"pro", "starter", "team"}) {
		t.Fatalf("IDs = %v", got)
	}
}
