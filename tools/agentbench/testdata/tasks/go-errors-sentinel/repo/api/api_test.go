package api

import (
	"errors"
	"testing"

	"example.com/kv/service"
	"example.com/kv/store"
)

func TestStatus(t *testing.T) {
	svc := service.Service{St: store.New()}
	if got := Status(svc.Rename("missing", "x")); got != 404 {
		t.Fatalf("rename missing: %d", got)
	}
	if _, err := svc.St.Put("a", "1", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Append("a", "2", 7); Status(err) != 409 {
		t.Fatalf("append stale: %d (%v)", Status(err), err)
	}
	if Status(errors.New("disk full")) != 500 || Status(nil) != 200 {
		t.Fatal("other statuses")
	}
}
