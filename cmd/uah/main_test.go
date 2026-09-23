package main_test

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var uahBin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "uah-cli")
	if err != nil {
		panic(err)
	}
	uahBin = filepath.Join(dir, "uah")
	if msg, err := exec.Command("go", "build", "-o", uahBin, ".").CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "build uah: %v\n%s", err, msg)
		os.Exit(1)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

type cliResult struct {
	code           int
	stdout, stderr string
}

func uah(t *testing.T, args ...string) cliResult {
	t.Helper()
	cmd := exec.Command(uahBin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	code := 0
	if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
		code = exitErr.ExitCode()
	} else {
		require.NoError(t, err)
	}

	return cliResult{code: code, stdout: stdout.String(), stderr: stderr.String()}
}

func TestVersion(t *testing.T) {
	for _, flag := range []string{"--version", "-v"} {
		res := uah(t, flag)
		assert.Equal(t, 0, res.code, flag)
		assert.Contains(t, res.stdout, "uah version", flag)
	}
}

func TestHelpListsCommands(t *testing.T) {
	res := uah(t, "--help")
	require.Equal(t, 0, res.code)
	assert.Contains(t, res.stdout, "run")
	assert.Contains(t, res.stdout, "sessions")
	assert.Contains(t, res.stdout, "a general-purpose harness for unreal-agent-runner")
}

func TestUnknownFlag(t *testing.T) {
	res := uah(t, "--no-such-flag")
	assert.Equal(t, 2, res.code)
	assert.Empty(t, res.stdout)
	assert.Equal(t, 1, strings.Count(res.stderr, "\n"), res.stderr)
	assert.Contains(t, res.stderr, "no-such-flag")
}

func TestNotImplemented(t *testing.T) {
	for _, args := range [][]string{nil, {"run"}, {"sessions"}} {
		res := uah(t, args...)
		assert.Equal(t, 1, res.code, args)
		assert.Contains(t, res.stderr, "not implemented yet", args)
	}
}
