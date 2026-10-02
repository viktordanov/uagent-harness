package seed

import (
	"testing"

	"example.com/bookmarks/internal/store"
)

func TestLoad(t *testing.T) {
	s := store.New()
	if err := Load(s); err != nil {
		t.Fatal(err)
	}
	if s.Len() != len(Demo) {
		t.Fatalf("Len %d, want %d", s.Len(), len(Demo))
	}
	if err := Load(s); err == nil {
		t.Fatal("loading twice did not report the duplicate URLs")
	}
}
