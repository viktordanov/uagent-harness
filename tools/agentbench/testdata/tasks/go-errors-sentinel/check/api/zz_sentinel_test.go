package api

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"example.com/kv/service"
	"example.com/kv/store"
)

func TestSentinels(t *testing.T) {
	svc := service.Service{St: store.New()}
	err := svc.Rename("ghost", "x")
	if !errors.Is(err, store.ErrNotFound) || !strings.Contains(err.Error(), `"ghost"`) {
		t.Fatalf("rename missing: %v", err)
	}
	if _, err := svc.Append("ghost", "t", 0); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("append missing: %v", err)
	}
	if _, err := svc.St.Put("k", "v", 0); err != nil {
		t.Fatal(err)
	}
	_, err = svc.Append("k", "w", 5)
	if !errors.Is(err, store.ErrConflict) || errors.Is(err, store.ErrNotFound) || !strings.Contains(err.Error(), `"k"`) {
		t.Fatalf("append stale: %v", err)
	}
	if _, err := svc.St.Put("taken", "v", 0); err != nil {
		t.Fatal(err)
	}
	if err := svc.Rename("k", "taken"); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("rename onto existing: %v", err)
	}
	if err := svc.St.Delete("nope"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("delete missing: %v", err)
	}
	// The API must not read messages: a wrapped sentinel with an unrelated
	// message still maps by identity, and a message alone does not.
	if Status(fmt.Errorf("weird: %w", store.ErrConflict)) != 409 {
		t.Fatal("wrapped conflict")
	}
	if Status(errors.New("user not found in cache")) != 500 {
		t.Fatal("Status matched a message instead of the error")
	}
}
