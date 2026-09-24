package app

import (
	"context"
	"fmt"

	"github.com/viktordanov/uagent-harness/internal/config"
	"github.com/viktordanov/uagent-harness/internal/config/tomledit"
)

// SaveSetting writes one key to the user file (in.ConfigPath) with the
// comment-preserving editor, as the TUI's /config does; a nil value removes
// it. A change that would stop a session from starting with these inputs,
// such as fast mode with the process engine, is undone and returned as the
// error.
func SaveSetting(ctx context.Context, in Inputs, key string, value any) error {
	before, mode, err := tomledit.Read(in.ConfigPath)
	if err != nil {
		return err //nolint:wrapcheck // Read names the file
	}
	if err := config.SetValue(in.ConfigPath, key, value); err != nil {
		return err //nolint:wrapcheck // SetValue names the file
	}
	in.SessionRef = ""
	if _, err := Inspect(ctx, in); err != nil {
		if rerr := tomledit.Write(in.ConfigPath, before, mode); rerr != nil {
			return fmt.Errorf("%w; restoring the file also failed: %w", err, rerr)
		}

		return fmt.Errorf("not saved: %w", err)
	}

	return nil
}
