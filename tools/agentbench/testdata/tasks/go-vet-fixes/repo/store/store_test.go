package store

import "testing"

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
