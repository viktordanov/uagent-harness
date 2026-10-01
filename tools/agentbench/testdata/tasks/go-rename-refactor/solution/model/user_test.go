package model

import "testing"

func TestNewAccount(t *testing.T) {
	u, err := NewAccount(1, " Ann@Example.com ", "Ann")
	if err != nil || u.Email != "ann@example.com" {
		t.Fatalf("NewAccount = %+v, %v", u, err)
	}
	if _, err := NewAccount(2, "nope", "Bob"); err != ErrInvalid {
		t.Fatalf("want ErrInvalid, got %v", err)
	}
}

func TestSortByName(t *testing.T) {
	us := []Account{{ID: 2, Name: "b"}, {ID: 3, Name: "a"}, {ID: 1, Name: "b"}}
	SortByName(us)
	if us[0].ID != 3 || us[1].ID != 1 || us[2].ID != 2 {
		t.Fatalf("order %+v", us)
	}
}
