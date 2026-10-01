package store

import (
	"testing"

	"example.com/accounts/model"
)

func TestPutGetList(t *testing.T) {
	s := New()
	s.Put(model.UserRecord{ID: 1, Name: "b"})
	s.Put(model.UserRecord{ID: 2, Name: "a"})
	if u, ok := s.Get(1); !ok || u.Name != "b" {
		t.Fatalf("Get = %+v %v", u, ok)
	}
	if l := s.List(); len(l) != 2 || l[0].ID != 2 {
		t.Fatalf("List = %+v", l)
	}
}
