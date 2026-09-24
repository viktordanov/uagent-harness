package session_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent-harness/internal/engine"
	"github.com/viktordanov/uagent-harness/internal/session"
)

func TestInDir(t *testing.T) {
	root := t.TempDir()
	proj, other := filepath.Join(root, "proj"), filepath.Join(root, "other")
	require.NoError(t, os.MkdirAll(filepath.Join(proj, "sub"), 0o700))
	require.NoError(t, os.MkdirAll(other, 0o700))
	link := filepath.Join(root, "link")
	require.NoError(t, os.Symlink(proj, link))
	infos := []session.Info{
		{ID: "a", Workspace: proj},
		{ID: "b", Workspace: filepath.Join(proj, "sub")},
		{ID: "c", Workspace: other},
		{ID: "d", Workspace: link},
		{ID: "e", Workspace: proj + "/./"},
	}

	got := session.InDir(infos, proj)

	ids := make([]string, 0, len(got))
	for _, in := range got {
		ids = append(ids, in.ID)
	}
	assert.Equal(t, []string{"a", "d", "e"}, ids, "exact directory after normalization; subdirectories do not match")
	assert.True(t, session.SameDir(link, proj+"/"))
}

func TestInteractive(t *testing.T) {
	infos := []session.Info{{ID: "a", Source: session.SourceTUI}, {ID: "b", Source: session.SourceRun}, {ID: "c"}}
	got := session.Interactive(infos)
	require.Len(t, got, 2)
	assert.Equal(t, "a", got[0].ID)
	assert.Equal(t, "c", got[1].ID, "sessions without a sidecar stay")
}

func TestSidecar(t *testing.T) {
	dir := t.TempDir()
	eng := newFakeEngine(engine.Capabilities{})
	s, err := session.Open(context.Background(), eng, session.Options{Settings: settings(), SessionsDir: dir, Source: session.SourceRun})
	require.NoError(t, err)
	require.NoError(t, s.Close())
	sc, found, err := session.ReadSidecar(dir, s.ID())
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, session.SourceRun, sc.Source)

	resumed, err := session.Open(context.Background(), eng, session.Options{ID: s.ID(), Resumed: true, Settings: settings(), SessionsDir: dir, Source: session.SourceTUI})
	require.NoError(t, err)
	require.NoError(t, resumed.Close())
	sc, _, err = session.ReadSidecar(dir, s.ID())
	require.NoError(t, err)
	assert.Equal(t, session.SourceRun, sc.Source, "the command that started the session decides")
}
