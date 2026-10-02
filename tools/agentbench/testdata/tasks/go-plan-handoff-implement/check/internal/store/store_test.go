package store

import (
	"errors"
	"testing"
)

func TestMemory(t *testing.T) {
	m := NewMemory()
	code, err := m.Put("https://example.com/a")
	if err != nil {
		t.Fatal(err)
	}
	if len(code) != 6 {
		t.Errorf("code %q is not six characters", code)
	}
	if got, err := m.Get(code); err != nil || got != "https://example.com/a" {
		t.Errorf("Get(%q) = %q, %v", code, got, err)
	}
	if _, err := m.Get("nope00"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown code: %v", err)
	}
}
