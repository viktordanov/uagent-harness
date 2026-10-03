package contextprep

import (
	"path"
	"slices"
	"strings"
)

// Shell families that more than one switch names.
const (
	famBash       = "bash"
	famNushell    = "nushell"
	famPowershell = "powershell"
	famCsh        = "csh"
	famCmd        = "cmd"
	keyPwsh       = "pwsh"
)

// shellFamily names the shell by its base name: bash, zsh, sh, fish,
// nushell, xonsh, elvish, powershell, cmd, csh, or the base name itself.
func shellFamily(shell string) string {
	name := strings.ToLower(path.Base(strings.ReplaceAll(shell, `\`, "/")))
	name = strings.TrimSuffix(name, ".exe")
	switch name {
	case famBash, "zsh", "fish", "xonsh", "elvish", famCmd:
		return name
	case "sh", "dash", "ash", "ksh", "mksh", "posh", "busybox":
		return "sh"
	case "nu", famNushell:
		return famNushell
	case keyPwsh, famPowershell:
		return famPowershell
	case famCsh, "tcsh":
		return famCsh
	}

	return name
}

// shellName is how the text names the shell ({{shell_name}}): its family.
func shellName(shell string) string { return shellFamily(shell) }

// shellKey is the shell's family as When.Shell names it: nu and pwsh for
// nushell and PowerShell, and other for a shell uah does not know.
func shellKey(shell string) string {
	f := shellFamily(shell)
	switch f {
	case famNushell:
		return "nu"
	case famPowershell:
		return keyPwsh
	}
	if slices.Contains(Shells, f) {
		return f
	}

	return "other"
}

// osName is how the text names the OS ({{os}}).
func osName(goos string) string {
	switch goos {
	case "darwin":
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
