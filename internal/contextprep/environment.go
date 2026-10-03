package contextprep

import (
	"context"
	"fmt"
	"path"
	"runtime"
	"strings"
)

// Environment names the operating system and the shell commands run in,
// with the constructs that commonly break in them. Models write POSIX sh
// and GNU flags by default, and a bare shell name does not stop them, so
// each case names the traps and what to write instead.
type Environment struct{}

// Name implements Adapter.
func (Environment) Name() string { return "environment" }

// Prepare implements Adapter. It always names the OS and the shell.
func (Environment) Prepare(_ context.Context, f Facts) string {
	shell := f.Shell
	if shell == "" {
		shell = "/bin/sh" // what uah runs commands with when $SHELL is unset
	}
	goos := f.GOOS
	if goos == "" {
		goos = runtime.GOOS
	}
	family := shellFamily(shell)

	var b strings.Builder
	fmt.Fprintf(&b, "Commands run in %s (%s -c) on %s.", family, shell, osName(goos))
	if traps := shellTraps(family, shell, goos); traps != "" {
		b.WriteString("\n" + traps)
	}
	if traps := osTraps(goos); traps != "" {
		b.WriteString("\n" + traps)
	}

	return b.String()
}

// Shell families shellFamily names, and the GOOS with the most to say.
const (
	nushell    = "nushell"
	powershell = "powershell"
	csh        = "csh"
	darwin     = "darwin"
)

// shellFamily names the shell by its basename: bash, zsh, sh, fish,
// nushell, xonsh, elvish, powershell, cmd, csh, or the basename itself.
func shellFamily(shell string) string {
	name := strings.ToLower(path.Base(strings.ReplaceAll(shell, `\`, "/")))
	name = strings.TrimSuffix(name, ".exe")
	switch name {
	case "bash", "zsh", "fish", "xonsh", "elvish", "cmd":
		return name
	case "sh", "dash", "ash", "ksh", "mksh", "posh", "busybox":
		return "sh"
	case "nu", nushell:
		return nushell
	case "pwsh", powershell:
		return powershell
	case csh, "tcsh":
		return csh
	}

	return name
}

// osName is how the prompt names goos.
func osName(goos string) string {
	switch goos {
	case darwin:
		return "macOS"
	case "linux":
		return "Linux"
	case "windows":
		return "Windows"
	case "freebsd":
		return "FreeBSD"
	case "openbsd":
		return "OpenBSD"
	case "netbsd":
		return "NetBSD"
	}

	return goos
}

// shellTraps is what to write instead of the POSIX constructs that fail in
// the shell's family, or "" when POSIX sh just works.
func shellTraps(family, shell, goos string) string {
	switch family {
	case "bash":
		if goos == darwin && shell == "/bin/bash" {
			return "macOS /bin/bash is 3.2: no declare -A, mapfile/readarray, ${x,,} or |&."
		}
		return ""
	case "sh":
		return "This is plain POSIX sh: [[ ]], arrays, source, {a,b}, <(…) and echo -e may not work; " +
			"use [ ], ., printf, or run bash -c '…'."
	case "zsh":
		return `zsh differs from bash:
- An unmatched glob is an error ("no matches found") and the command does not run: quote patterns and URLs with *, ?, [ (find -name '*.go', curl 'https://x?a=1').
- Unquoted $var is not word-split: use ${=var} or an array.
- Arrays index from 1; read -a is read -A.`
	case "fish":
		return `fish is not a POSIX shell. These fail in fish:
- Heredocs (<<EOF, <<<): pipe printf '%s\n' … into the command, or write the file with your file tool.
- for …; do …; done, while …; do …; done, if …; then …; fi, case: fish uses for x in …; …; end, while …; …; end, if …; …; end, switch.
- x=1 on its own: use set x 1 (set -x to export). VAR=1 cmd works.
- $?, ${x}, $((1+2)), backticks, <(cmd): use $status, {$x}, math 1+2, (cmd), (cmd | psub).
- An unmatched glob is an error: quote patterns (find -name '*.go').
For a POSIX script, run it as one sh -c '…' (inside fish single quotes, \' and \\ are escapes).`
	case nushell:
		return `nushell is not a POSIX shell; its commands return structured data. These fail in nushell:
- && and ||: separate commands with ;.
- x=1, export X=1, $X: use $env.X = '1' and $env.X.
- $(cmd), backticks: use (cmd).
- 2>&1, 2>/dev/null: use o+e>| and e> /dev/null.
- Heredocs, for …; do …; done, if …; then …; fi.
ls, rm, cp and others are nushell builtins with other flags: prefix ^ (^ls) for the external command. For a POSIX script, run it as one sh -c '…'.`
	case "xonsh":
		return `xonsh is Python-based, not a POSIX shell. Simple commands, pipes, && and || work; these fail:
- Heredocs, for …; do …; done, if …; then …; fi: use Python syntax.
- x=1, export X=1: use $X = '1'.
- Backticks are regex globs, not command substitution: use $(cmd).
For a POSIX script, run it as one sh -c '…'.`
	case "elvish":
		return `elvish is not a POSIX shell. These fail in elvish:
- && and ||: separate commands with ; (a failing command throws and stops the rest).
- x=1, export X=1, $X: use var x = 1, set E:X = 1, $E:X.
- $(cmd), backticks: use (cmd).
- Heredocs, for …; do …; done, if …; then …; fi.
For a POSIX script, run it as one sh -c '…'.`
	case powershell:
		return `This is PowerShell, not a POSIX shell. These fail or differ:
- export X=1, X=1 cmd, $X: use $env:X = '1' and $env:X.
- Heredocs: use a here-string, @' and '@ on their own lines.
- The backtick is the escape character; $(…) is a subexpression.
- 2>/dev/null: use 2>$null. && and || need PowerShell 7.
- ls, rm, cp, cat (and curl in Windows PowerShell) are aliases of cmdlets with other flags: rm -rf is Remove-Item -Recurse -Force.`
	case "cmd":
		return `This is cmd.exe, not a POSIX shell: use set X=1 and %X%, double quotes only (single quotes are literal), 2>nul, and no heredocs or POSIX loops. For anything more, run powershell -Command "…".`
	case csh:
		return `csh is not a POSIX shell. These fail in csh:
- x=1, export X=1, X=1 cmd: use set x = 1, setenv X 1, env X=1 cmd.
- $(cmd): use backticks.
- 2>&1: use >& or |&.
- for …; do …; done, if …; then …; fi: use foreach … end, if (…) then … endif.
For a POSIX script, run it as one sh -c '…'.`
	}

	return family + " may not be a POSIX shell: if POSIX syntax fails, run it as one sh -c '…'."
}

// osTraps is what commonly breaks in the OS's userland, or "".
func osTraps(goos string) string {
	switch goos {
	case darwin:
		return "macOS has BSD tools, not GNU: sed -i '' (not sed -i), stat -f (not -c), date -v-1d (not -d), " +
			"no grep -P (use -E or perl), no nproc (sysctl -n hw.ncpu)."
	case "freebsd", "openbsd", "netbsd", "dragonfly":
		return "BSD tools, not GNU: flags of sed -i, stat, date and grep differ from GNU's."
	case "windows":
		return "Windows paths use \\ and drive letters; Unix tools such as grep, sed and find may be missing."
	}

	return ""
}
