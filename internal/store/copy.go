package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// dbFile is the index's file name in a state directory.
const dbFile = "uah.db"

// CopyIndex copies the index of the state directory from into the directory
// dir, for the state directory stateDir: the copy's run rows point at
// stateDir's run records. dir must not have an index yet. Without an index
// in from, it copies nothing.
//
// SQLite writes one consistent snapshot (VACUUM INTO) and from is only read:
// while another process has the index open, its WAL is read through, never
// copied; otherwise the index is opened immutable, so reading it creates no
// WAL or shared-memory file next to it.
func CopyIndex(ctx context.Context, from, dir, stateDir string) error {
	src := filepath.Join(from, dbFile)
	if _, err := os.Stat(src); errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	dsn := "file:" + src + "?mode=ro&_pragma=busy_timeout(5000)"
	if _, err := os.Stat(src + "-wal"); errors.Is(err, fs.ErrNotExist) {
		dsn = "file:" + src + "?mode=ro&immutable=1"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return fmt.Errorf("failed to open the index: %w", err)
	}
	dst := filepath.Join(dir, dbFile)
	_, err = db.ExecContext(ctx, `VACUUM INTO ?`, dst)
	if closeErr := db.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("failed to copy the index: %w", err)
	}
	if db, err = sql.Open("sqlite", "file:"+dst); err != nil {
		return fmt.Errorf("failed to open the copied index: %w", err)
	}
	defer db.Close()
	runs := filepath.Join(stateDir, "runs") + string(filepath.Separator)
	if _, err := db.ExecContext(ctx, `UPDATE runs SET dir = ? || run_id`, runs); err != nil {
		return fmt.Errorf("failed to update the copied index: %w", err)
	}

	return nil
}
