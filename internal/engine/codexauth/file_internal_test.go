package codexauth

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLock_ExcludesAnotherHolder: the lock is a flock on a file beside the
// auth file, so another uah process (here, another open file description)
// waits for it.
func TestLock_ExcludesAnotherHolder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	unlock, err := fileFor(path).lock(context.Background())
	require.NoError(t, err)
	other := &file{path: path}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	_, err = other.lock(ctx)
	require.ErrorIs(t, err, context.DeadlineExceeded)

	unlock()
	unlock2, err := other.lock(context.Background())
	require.NoError(t, err)
	unlock2()
	info, err := os.Stat(filepath.Join(filepath.Dir(path), ".auth.json.uah-lock"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestUpdated_KeepsOrderAndAddsMissingFields(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 500, time.UTC)
	out, err := updated([]byte(`{"x":1,"tokens":{"account_id":"a","access_token":"old"}}`), tokens{AccessToken: "new", RefreshToken: "rt"}, now)
	require.NoError(t, err)
	assert.Equal(t, `{
  "x": 1,
  "tokens": {
    "account_id": "a",
    "access_token": "new",
    "refresh_token": "rt"
  },
  "last_refresh": "2026-09-28T12:00:00.0000005Z"
}`, string(out))

	_, err = updated([]byte(`[1]`), tokens{AccessToken: "new"}, now)
	require.Error(t, err)
}
