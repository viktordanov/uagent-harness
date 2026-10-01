package service

import (
	"testing"

	"example.com/accounts/store"
)

func TestRegisterPromote(t *testing.T) {
	s := New(store.New())
	u, err := s.Register("a@x.io", "Ann")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Register("A@x.io", "Again"); err != ErrExists {
		t.Fatalf("want ErrExists, got %v", err)
	}
	if p, ok := s.Promote(u.ID); !ok || !p.Admin {
		t.Fatalf("Promote = %+v %v", p, ok)
	}
}
