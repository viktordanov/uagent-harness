package fetch

import (
	"context"
	"errors"
	"testing"

	"example.com/fetcher/config"
)

func flaky(failures int, calls *int) Getter {
	return func(context.Context, string) ([]byte, error) {
		*calls++
		if *calls <= failures {
			return nil, errors.New("boom")
		}

		return []byte("ok"), nil
	}
}

func TestFetchRetriesHidden(t *testing.T) {
	c := config.Default()
	c.Retries = 2
	calls := 0
	if body, err := Fetch(context.Background(), c, flaky(2, &calls), "u"); err != nil || string(body) != "ok" || calls != 3 {
		t.Fatalf("2 failures, 2 retries: %q, %v after %d calls", body, err, calls)
	}
	calls = 0
	if _, err := Fetch(context.Background(), c, flaky(5, &calls), "u"); err == nil || calls != 3 {
		t.Fatalf("5 failures, 2 retries: %v after %d calls, want an error after 3", err, calls)
	}
	c.Retries = 0
	calls = 0
	if _, err := Fetch(context.Background(), c, flaky(1, &calls), "u"); err == nil || calls != 1 {
		t.Fatalf("no retries: %v after %d calls", err, calls)
	}
}
