package migrate_test

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "modernc.org/sqlite"

	"github.com/viktordanov/uagent/core"
	uaharness "github.com/viktordanov/uagent/harness"
	"github.com/viktordanov/uagent/testing/fixtures"

	"github.com/viktordanov/uagent-harness/internal/home"
	"github.com/viktordanov/uagent-harness/internal/home/migrate"
	"github.com/viktordanov/uagent-harness/internal/store"
	"github.com/viktordanov/uagent-harness/testing/harnesstest"
)

// oldHome lays out the folders uah used before ~/.uah under a fresh HOME:
// a configuration directory with a config file, instructions, and hook
// trust, and a state directory with one real run, an image, a model cache,
// a lock, logs, and sandbox scratch files.
func oldHome(t *testing.T) (migrate.Paths, *harnesstest.Env) {
	t.Helper()
	userHome := t.TempDir()
	t.Setenv("HOME", userHome)
	t.Setenv(home.Env, "")
	for _, name := range []string{"XDG_CONFIG_HOME", "XDG_STATE_HOME", "UAGENT_STATE_DIR"} {
		t.Setenv(name, "")
	}
	p := migrate.Old(os.Getenv, userHome)
	write(t, filepath.Join(p.Config, "config.toml"), "effort = \"low\"\n")
	write(t, filepath.Join(p.Config, "AGENTS.md"), "Answer in haiku.")
	write(t, filepath.Join(p.Config, "trusted-hooks.json"), "{}")

	env := harnesstest.NewEnv(t)
	env.StateDir = p.State
	t.Setenv("FAKERUNNER_FIXTURE", fixtures.Path("simple.jsonl"))
	h := uaharness.New(uaharness.Config{RunnerPath: harnesstest.FakeRunner(t), StateDir: p.State, Getenv: env.Getenv})
	_, err := h.Run(context.Background(), core.Request{
		Messages: []core.UserInput{{Text: "summarize the parser"}},
		Provider: "openai-codex", Model: "gpt-6-sol", Effort: "high", Workspace: env.Workspace,
	}, func(core.Event) {})
	require.NoError(t, err)
	ix, err := store.Open(context.Background(), p.State)
	require.NoError(t, err)
	require.NoError(t, ix.Close())
	write(t, filepath.Join(p.State, "images", "abc.png"), "png")
	write(t, filepath.Join(p.State, "models", "openai.json"), "[]")
	write(t, filepath.Join(p.State, "sessions", "s1.lock"), "")
	write(t, filepath.Join(p.State, "logs", "uah-tui.log"), "log")
	write(t, filepath.Join(p.State, "sandbox", "gate.sh"), "#!/bin/sh")

	return p, env
}

func write(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

// snapshot lists every file under dirs with its size and modification time.
func snapshot(t *testing.T, dirs ...string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, dir := range dirs {
		require.NoError(t, filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			out[path] = fmt.Sprint(info.ModTime(), info.Mode(), info.Size())

			return nil
		}))
	}

	return out
}

func TestAuto_CopiesOnce(t *testing.T) {
	p, _ := oldHome(t)
	before := snapshot(t, p.Config, p.State)

	migrated, err := migrate.Auto(context.Background(), os.Getenv)
	require.NoError(t, err)
	require.True(t, migrated)

	assert.Equal(t, before, snapshot(t, p.Config, p.State), "the old folders are untouched")
	for _, name := range []string{"config.toml", "AGENTS.md", "trusted-hooks.json", "images/abc.png", "models/openai.json", "uah.db", migrate.Marker} {
		assert.FileExists(t, filepath.Join(p.Home, name))
	}
	for _, name := range []string{"logs", "sandbox", "sessions/s1.lock"} {
		assert.NoFileExists(t, filepath.Join(p.Home, name))
	}
	oldSummary := summaries(t, p.State)
	assert.Equal(t, oldSummary, summaries(t, p.Home), "run records keep their modification times")
	rec, found, err := migrate.ReadRecord(p.Home)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, p.Config, rec.Config)
	assert.Equal(t, p.State, rec.State)

	infos, err := store.List(context.Background(), p.Home)
	require.NoError(t, err)
	require.Len(t, infos, 1)
	assert.Equal(t, "summarize the parser", infos[0].FirstPrompt)
	assertRunDirs(t, p.Home)

	write(t, filepath.Join(p.Config, "config.toml"), "effort = \"high\"\n")
	migrated, err = migrate.Auto(context.Background(), os.Getenv)
	require.NoError(t, err)
	assert.False(t, migrated, "an existing home is never migrated into")
	data, err := os.ReadFile(filepath.Join(p.Home, "config.toml"))
	require.NoError(t, err)
	assert.Equal(t, "effort = \"low\"\n", string(data))
}

// summaries maps each run's ID to its summary's modification time.
func summaries(t *testing.T, state string) map[string]time.Time {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(state, "runs"))
	require.NoError(t, err)
	out := map[string]time.Time{}
	for _, e := range entries {
		info, err := os.Stat(filepath.Join(state, "runs", e.Name(), uaharness.SummaryFile))
		require.NoError(t, err)
		out[e.Name()] = info.ModTime()
	}

	return out
}

// assertRunDirs checks that the copied index points at the new run records.
func assertRunDirs(t *testing.T, state string) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(state, "uah.db"))
	require.NoError(t, err)
	defer db.Close()
	var dir string
	require.NoError(t, db.QueryRowContext(context.Background(), `SELECT dir FROM runs LIMIT 1`).Scan(&dir))
	assert.Equal(t, filepath.Join(state, "runs"), filepath.Dir(dir))
}

func TestAuto_Skips(t *testing.T) {
	t.Run("UAH_HOME set", func(t *testing.T) {
		p, _ := oldHome(t)
		t.Setenv(home.Env, filepath.Join(t.TempDir(), "custom"))
		migrated, err := migrate.Auto(context.Background(), os.Getenv)
		require.NoError(t, err)
		assert.False(t, migrated)
		assert.NoDirExists(t, p.Home)
		assert.NoDirExists(t, home.Dir())
	})
	t.Run("nothing to copy", func(t *testing.T) {
		userHome := t.TempDir()
		t.Setenv("HOME", userHome)
		t.Setenv(home.Env, "")
		migrated, err := migrate.Auto(context.Background(), func(string) string { return "" })
		require.NoError(t, err)
		assert.False(t, migrated)
		assert.NoDirExists(t, filepath.Join(userHome, ".uah"))
	})
}

func TestOld_Variables(t *testing.T) {
	env := map[string]string{"XDG_CONFIG_HOME": "/xdg/config", "XDG_STATE_HOME": "/xdg/state"}
	p := migrate.Old(func(k string) string { return env[k] }, "/home/me")
	assert.Equal(t, migrate.Paths{Home: "/home/me/.uah", Config: "/xdg/config/uagent", State: "/xdg/state/unreal-agent"}, p)

	env["UAGENT_STATE_DIR"] = "/custom/state"
	assert.Equal(t, "/custom/state", migrate.Old(func(k string) string { return env[k] }, "/home/me").State)
}

func TestRun_FailureLeavesNoHome(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads unreadable files")
	}
	p, _ := oldHome(t)
	secret := filepath.Join(p.Config, "mcp-credentials.json")
	write(t, secret, "{}")
	require.NoError(t, os.Chmod(secret, 0o000))
	t.Cleanup(func() { _ = os.Chmod(secret, 0o600) })
	before := snapshot(t, p.Config, p.State)

	migrated, err := migrate.Run(context.Background(), p)
	require.Error(t, err)
	assert.False(t, migrated)
	assert.Contains(t, err.Error(), "mcp-credentials.json")
	assert.NoDirExists(t, p.Home)
	leftovers, err := filepath.Glob(filepath.Join(filepath.Dir(p.Home), ".uah.migrating-*"))
	require.NoError(t, err)
	assert.Empty(t, leftovers, "the partial copy is removed")
	assert.Equal(t, before, snapshot(t, p.Config, p.State))
}

// TestRun_IndexWithAnOpenWriter copies the index while another uah holds a
// write transaction: the copy has what was committed and none of the rest.
func TestRun_IndexWithAnOpenWriter(t *testing.T) {
	p, _ := oldHome(t)
	ctx := context.Background()
	ix, err := store.Open(ctx, p.State)
	require.NoError(t, err)
	defer ix.Close()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(p.State, "uah.db")+"?_pragma=busy_timeout(5000)")
	require.NoError(t, err)
	defer db.Close()
	conn, err := db.Conn(ctx)
	require.NoError(t, err)
	defer conn.Close()
	_, err = conn.ExecContext(ctx, `BEGIN IMMEDIATE`)
	require.NoError(t, err)
	_, err = conn.ExecContext(ctx, `INSERT INTO meta (key, value) VALUES ('uncommitted', 'yes')`)
	require.NoError(t, err)

	migrated, err := migrate.Run(ctx, p)
	require.NoError(t, err)
	require.True(t, migrated)
	_, err = conn.ExecContext(ctx, `ROLLBACK`)
	require.NoError(t, err)
	for _, suffix := range []string{"-wal", "-shm"} {
		assert.NoFileExists(t, filepath.Join(p.Home, "uah.db"+suffix), "the WAL is read, not copied")
	}

	copied, err := sql.Open("sqlite", "file:"+filepath.Join(p.Home, "uah.db"))
	require.NoError(t, err)
	defer copied.Close()
	var runs, uncommitted int
	require.NoError(t, copied.QueryRowContext(ctx, `SELECT count(*) FROM runs`).Scan(&runs))
	require.NoError(t, copied.QueryRowContext(ctx, `SELECT count(*) FROM meta WHERE key = 'uncommitted'`).Scan(&uncommitted))
	assert.Equal(t, 1, runs)
	assert.Zero(t, uncommitted)
}
