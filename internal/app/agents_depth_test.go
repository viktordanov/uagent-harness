package app_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent-harness/internal/app"
)

// TestSetup_MaxDepthIsOne clamps a configured max_depth above 1 with a
// notice: subagents never start subagents.
func TestSetup_MaxDepthIsOne(t *testing.T) {
	_, in := setupEnv(t)
	require.NoError(t, os.MkdirAll(filepath.Dir(in.ConfigPath), 0o700))
	require.NoError(t, os.WriteFile(in.ConfigPath, []byte("[agents]\nmax_depth = 3\n"), 0o600))

	res, err := app.Setup(context.Background(), in, io.Discard)

	require.NoError(t, err)
	assert.Contains(t, strings.Join(res.Options.Notices, "\n"), "agents.max_depth = 3 is above 1: subagents never start subagents, so it is 1")
}
