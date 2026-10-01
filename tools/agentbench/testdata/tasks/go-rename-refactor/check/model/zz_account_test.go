package model

import "testing"

func TestAccountRenamed(t *testing.T) {
	var a Account
	a, err := NewAccount(9, "z@z.z", "Zed")
	if err != nil || a.Display() != "Zed <z@z.z>" {
		t.Fatalf("NewAccount = %+v %v", a, err)
	}
}
