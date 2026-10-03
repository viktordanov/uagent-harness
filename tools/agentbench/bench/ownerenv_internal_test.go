package bench

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewEnvOwner: with -owner-env only the harness gets the owner's
// environment; the fixtures and the checks keep the bench's.
func TestNewEnvOwner(t *testing.T) {
	t.Setenv("GOFLAGS", "-mod=mod")
	e, err := newEnv(context.Background(), Config{Work: t.TempDir(), Mode: ModeAuto, OwnerEnv: true, Shell: "/bin/sh"})
	require.NoError(t, err)
	assert.Contains(t, e.base, "GOFLAGS=-count=1")
	assert.Contains(t, e.base, "GOPROXY=off")
	assert.Contains(t, e.harness, "SHELL=/bin/sh")
	assert.Contains(t, e.harness, "GOFLAGS=-mod=mod")
	assert.NotContains(t, e.harness, "GOFLAGS=-count=1")
	assert.NotContains(t, e.harness, "GOPROXY=off")
	assert.NotContains(t, e.harness, "TMPDIR="+e.tmp)

	plain, err := newEnv(context.Background(), Config{Work: t.TempDir(), Mode: ModeAuto})
	require.NoError(t, err)
	assert.Equal(t, plain.base, plain.harness)

	_, err = newEnv(context.Background(), Config{Work: t.TempDir(), OwnerEnv: true, Shell: "/no/such/shell"})
	require.Error(t, err)
}
