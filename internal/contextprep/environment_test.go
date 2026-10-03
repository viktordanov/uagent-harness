package contextprep_test

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uah/internal/contextprep"
)

func TestEnvironmentName(t *testing.T) {
	t.Parallel()
	var a contextprep.Adapter = contextprep.Environment{}
	assert.Equal(t, "environment", a.Name())
}

// TestEnvironmentPrepare pins each case's first line and the traps it names.
func TestEnvironmentPrepare(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		shell, goos string
		first       string
		has         []string
		hasNot      []string
	}{
		{
			shell: "/bin/bash", goos: "linux", first: "Commands run in bash (/bin/bash -c) on Linux.",
			hasNot: []string{"\n"},
		},
		{
			shell: "/bin/bash", goos: "darwin", first: "Commands run in bash (/bin/bash -c) on macOS.",
			has: []string{"3.2", "declare -A", "sed -i ''", "stat -f", "date -v-1d", "grep -P", "sysctl -n hw.ncpu"},
		},
		{
			shell: "/opt/homebrew/bin/bash", goos: "darwin", first: "Commands run in bash (/opt/homebrew/bin/bash -c) on macOS.",
			has: []string{"sed -i ''"}, hasNot: []string{"3.2"},
		},
		{
			shell: "/bin/zsh", goos: "darwin", first: "Commands run in zsh (/bin/zsh -c) on macOS.",
			has: []string{"unmatched glob", "no matches found", "${=var}", "read -A", "sed -i ''"},
		},
		{
			shell: "/usr/bin/zsh", goos: "linux", first: "Commands run in zsh (/usr/bin/zsh -c) on Linux.",
			has: []string{"unmatched glob"}, hasNot: []string{"BSD"},
		},
		{
			shell: "/opt/homebrew/bin/fish", goos: "darwin", first: "Commands run in fish (/opt/homebrew/bin/fish -c) on macOS.",
			has: []string{
				"not a POSIX shell", "<<EOF", "do …; done", "end", "set x 1", "set -x", "VAR=1 cmd works",
				"$status", "{$x}", "math 1+2", "(cmd | psub)", "unmatched glob", "sh -c '…'", "sed -i ''",
			},
		},
		{
			shell: "/usr/bin/fish", goos: "linux", first: "Commands run in fish (/usr/bin/fish -c) on Linux.",
			has: []string{"set x 1"}, hasNot: []string{"BSD"},
		},
		{shell: "/bin/dash", goos: "linux", first: "Commands run in sh (/bin/dash -c) on Linux.", has: []string{"[[ ]]", "bash -c '…'"}},
		{shell: "", goos: "linux", first: "Commands run in sh (/bin/sh -c) on Linux.", has: []string{"POSIX sh"}},
		{
			shell: "/usr/local/bin/nu", goos: "linux", first: "Commands run in nushell (/usr/local/bin/nu -c) on Linux.",
			has: []string{"&& and ||", "$env.X = '1'", "(cmd)", "o+e>|", "^ls", "sh -c '…'"},
		},
		{
			shell: "/usr/bin/xonsh", goos: "linux", first: "Commands run in xonsh (/usr/bin/xonsh -c) on Linux.",
			has: []string{"Python", "$X = '1'", "regex globs", "$(cmd)"},
		},
		{
			shell: "/usr/bin/elvish", goos: "linux", first: "Commands run in elvish (/usr/bin/elvish -c) on Linux.",
			has: []string{"var x = 1", "set E:X = 1", "(cmd)"},
		},
		{
			shell: `C:\Program Files\PowerShell\7\pwsh.exe`, goos: "windows",
			first: `Commands run in powershell (C:\Program Files\PowerShell\7\pwsh.exe -c) on Windows.`,
			has:   []string{"$env:X = '1'", "here-string", "2>$null", "Remove-Item -Recurse -Force", "drive letters"},
		},
		{
			shell: `C:\Windows\System32\cmd.exe`, goos: "windows",
			first: `Commands run in cmd (C:\Windows\System32\cmd.exe -c) on Windows.`,
			has:   []string{"set X=1", "%X%", "2>nul"},
		},
		{
			shell: "/bin/tcsh", goos: "freebsd", first: "Commands run in csh (/bin/tcsh -c) on FreeBSD.",
			has: []string{"setenv X 1", "foreach", ">&", "BSD tools"},
		},
		{
			shell: "/usr/bin/oddsh", goos: "plan9", first: "Commands run in oddsh (/usr/bin/oddsh -c) on plan9.",
			has: []string{"may not be a POSIX shell", "sh -c '…'"},
		},
	} {
		got := contextprep.Environment{}.Prepare(context.Background(), contextprep.Facts{Shell: tc.shell, GOOS: tc.goos})
		first, _, _ := strings.Cut(got, "\n")
		assert.Equal(t, tc.first, first, tc.shell)
		for _, s := range tc.has {
			assert.Contains(t, got, s, tc.shell+" on "+tc.goos)
		}
		for _, s := range tc.hasNot {
			assert.NotContains(t, got, s, tc.shell+" on "+tc.goos)
		}
	}
}

// TestShellClaims runs what the fish and zsh guidance says fails and what
// it says to write instead, in the shells installed here.
func TestShellClaims(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		shell      string
		fail, pass []string
	}{
		{
			shell: "fish",
			fail: []string{
				"cat <<EOF\nx\nEOF", "for i in 1 2; do echo $i; done", "if true; then echo y; fi",
				"x=1", "echo $?", "echo ${HOME}", "echo $((1+2))", "echo *.no-such-glob",
			},
			pass: []string{
				"printf '%s\\n' x | cat", "for i in 1 2; echo $i; end", "if true; echo y; end",
				"set x 1; echo $x", "set -x X 1; sh -c 'test \"$X\" = 1'", "X=1 sh -c 'test \"$X\" = 1'",
				"false; test $status = 1", "echo {$HOME}", "math 1+2", "echo (echo sub)",
				"diff (echo a | psub) (echo a | psub)", "true && echo y || echo n",
				"sh -c 'for i in 1 2; do x=$i; done; echo $x'", `test 'it\'s' = "it's"`,
			},
		},
		{
			shell: "zsh",
			fail:  []string{"echo *.no-such-glob", "read -a a <<< 'x y'"},
			pass:  []string{`x="a b"; set -- $x; test $# = 1`, `x="a b"; set -- ${=x}; test $# = 2`, "a=(p q); test $a[1] = p", "read -A a <<< 'x y'"},
		},
	} {
		path, err := exec.LookPath(tc.shell)
		if err != nil {
			t.Logf("%s is not installed", tc.shell)
			continue
		}
		for _, c := range tc.fail {
			out, err := exec.Command(path, "-c", c).CombinedOutput()
			assert.Error(t, err, "%s -c %q: %s", tc.shell, c, out)
		}
		for _, c := range tc.pass {
			out, err := exec.Command(path, "-c", c).CombinedOutput()
			require.NoError(t, err, "%s -c %q: %s", tc.shell, c, out)
		}
	}
}
