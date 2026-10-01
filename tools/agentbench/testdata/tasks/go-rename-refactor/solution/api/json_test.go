package api

import (
	"testing"

	"example.com/accounts/model"
)

func TestEncode(t *testing.T) {
	b, err := Encode(model.Account{ID: 7, Email: "a@b.c", Name: "A"})
	if err != nil || string(b) != `{"id":7,"email":"a@b.c","name":"A"}` {
		t.Fatalf("Encode = %s %v", b, err)
	}
}
