package fetch

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestWithDeadline(t *testing.T) {
	v, err := WithDeadline(context.Background(), time.Second, func(context.Context) (string, error) { return "ok", nil })
	if err != nil || v != "ok" {
		t.Fatalf("got %q, %v", v, err)
	}
	_, err = WithDeadline(context.Background(), 20*time.Millisecond, func(ctx context.Context) (string, error) {
		<-ctx.Done()
		time.Sleep(50 * time.Millisecond)

		return "late", nil
	})
	if !errors.Is(err, ErrSlow) {
		t.Fatalf("err = %v", err)
	}
}
