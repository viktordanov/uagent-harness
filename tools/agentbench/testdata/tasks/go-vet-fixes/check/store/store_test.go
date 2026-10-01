package store

import (
	"strings"
	"testing"
)

func TestSetGet(t *testing.T) {
	var s Store
	s.Set("a", "1")
	if v, ok := s.Get("a"); !ok || v != "1" {
		t.Fatalf("Get = %q, %v", v, ok)
	}
}

func TestClamp(t *testing.T) {
	if Clamp(5, 0, 3) != 3 || Clamp(-1, 0, 3) != 0 || Clamp(2, 0, 3) != 2 {
		t.Fatal("Clamp")
	}
}

func TestSign(t *testing.T) {
	if Sign(-4) != -1 || Sign(0) != 0 || Sign(9) != 1 {
		t.Fatal("Sign")
	}
}

func TestExportAndDescribe(t *testing.T) {
	var s Store
	s.Set("b", "2")
	s.Set("a", "1")
	s.Get("a")
	got, err := s.Export()
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `[{"key":"a","value":"1"},{"key":"b","value":"2"}]` {
		t.Fatalf("Export = %s", got)
	}
	snap := s.Snapshot()
	if len(snap) != 2 || snap["b"] != "2" {
		t.Fatalf("Snapshot = %v", snap)
	}
	if d := s.Describe(); !strings.HasPrefix(d, "2 keys, 1 hits") || !strings.Contains(d, "store") {
		t.Fatalf("Describe = %q", d)
	}
}
