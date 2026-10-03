package bench_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/viktordanov/uah/tools/agentbench/bench"
)

// TestOwnerEnv: the owner's environment keeps the user's Go and temporary
// directory settings, adds none of the bench's, puts the login shell in
// SHELL, and still drops what steers a harness or reaches the launcher.
func TestOwnerEnv(t *testing.T) {
	environ := []string{
		"PATH=/usr/bin", "HOME=/Users/o", "SHELL=/bin/zsh", "TMPDIR=/tmp/sandboxed",
		"GOCACHE=/Users/o/Library/Caches/go-build", "GOFLAGS=-mod=mod", "GOPATH=/Users/o/go",
		"UAH_ADAPTIVE_EFFORT=off", "OPENAI_API_KEY=x", "CLAUDECODE=1", "WEBTTY_SESSION_ID=s", "NO_COLOR=",
	}
	env := bench.OwnerEnv(environ, "/opt/homebrew/bin/fish", "/var/folders/xy/T/")
	get := func(name string) []string {
		var vals []string
		for _, kv := range env {
			if v, ok := strings.CutPrefix(kv, name+"="); ok {
				vals = append(vals, v)
			}
		}

		return vals
	}
	assert.Equal(t, []string{"/opt/homebrew/bin/fish"}, get("SHELL"))
	assert.Equal(t, []string{"/var/folders/xy/T/"}, get("TMPDIR"), "the login session's, not the launcher's")
	assert.Equal(t, []string{"/Users/o/Library/Caches/go-build"}, get("GOCACHE"))
	assert.Equal(t, []string{"-mod=mod"}, get("GOFLAGS"), "no -count=1")
	assert.Equal(t, []string{"/Users/o/go"}, get("GOPATH"))
	assert.Empty(t, get("GOPROXY"), "no GOPROXY=off")
	assert.Empty(t, get("GOTOOLCHAIN"))
	assert.Equal(t, []string{"1"}, get("NO_COLOR"))
	for _, dropped := range []string{"UAH_ADAPTIVE_EFFORT", "OPENAI_API_KEY", "CLAUDECODE", "WEBTTY_SESSION_ID"} {
		assert.Empty(t, get(dropped), dropped)
	}
	assert.True(t, slices.Contains(env, "PATH=/usr/bin"))

	env = bench.OwnerEnv(environ, "/bin/bash", "")
	assert.Contains(t, env, "TMPDIR=/tmp/sandboxed", "no login temporary directory keeps the inherited one")
}

func TestParseLoginShell(t *testing.T) {
	assert.Equal(t, "/opt/homebrew/bin/fish", bench.ParseLoginShell("UserShell: /opt/homebrew/bin/fish\n"))
	assert.Equal(t, "/usr/bin/fish", bench.ParseLoginShell("o:x:1000:1000:Owner,,,:/home/o:/usr/bin/fish\n"))
	assert.Empty(t, bench.ParseLoginShell("No such key: UserShell"))
}
