package app_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent-harness/internal/app"
	"github.com/viktordanov/uagent-harness/internal/home/migrate"
)

// TestDoctor_Home reports the home, whether the old folders were copied
// into it, variables uah no longer reads, and an old .uagent project
// directory.
func TestDoctor_Home(t *testing.T) {
	e, in := setupEnv(t)
	userHome := t.TempDir()
	t.Setenv("HOME", userHome)
	t.Setenv("UAH_HOME", "")
	env := map[string]string{"UAGENT_STATE_DIR": "/old/state"}
	opts := doctorOptions(t, filepath.Join(t.TempDir(), "trust.json"))
	opts.Getenv = func(k string) string { return env[k] }
	uah := filepath.Join(userHome, ".uah")
	doctor := func() []app.Check { return app.Doctor(context.Background(), in, opts) }

	checks := doctor()
	variables := find(t, checks, "environment")
	assert.Equal(t, app.CheckWarn, variables.Status)
	assert.Equal(t, "UAGENT_STATE_DIR is no longer read; set UAH_STATE_DIR instead", variables.Detail)
	assert.Equal(t, app.CheckOK, find(t, checks, "home").Status, "no old folders")

	delete(env, "UAGENT_STATE_DIR")
	writeFile(t, filepath.Join(userHome, ".config", "uagent", "config.toml"), "")
	failed := find(t, doctor(), "home")
	assert.Equal(t, app.CheckFail, failed.Status, "the old folders are there and the home is not: the copy failed")
	assert.Contains(t, failed.Fix, "run uah again")

	require.NoError(t, os.Mkdir(uah, 0o700))
	skipped := find(t, doctor(), "home")
	assert.Equal(t, app.CheckWarn, skipped.Status)
	assert.Contains(t, skipped.Detail, "was not copied from")

	require.NoError(t, os.Remove(uah))
	_, err := migrate.Run(context.Background(), migrate.Old(opts.Getenv, userHome))
	require.NoError(t, err)
	copied := find(t, doctor(), "home")
	assert.Equal(t, app.CheckOK, copied.Status)
	assert.Contains(t, copied.Detail, "copied from "+filepath.Join(userHome, ".config", "uagent"))

	require.NoError(t, os.Mkdir(filepath.Join(e.Workspace, ".uagent"), 0o700))
	project := find(t, doctor(), "project config")
	assert.Equal(t, app.CheckWarn, project.Status)
	assert.Contains(t, project.Detail, ".uagent is no longer read")
	assert.Equal(t, "run `mv .uagent .uah` in "+e.Workspace, project.Fix)
}
