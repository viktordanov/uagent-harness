package bench

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"runtime"
	"strings"
)

// OwnerEnv is the environment a harness gets with -owner-env: the user's
// own, as an interactive session has it, with the login shell as SHELL and
// tmp as TMPDIR when it is not "". It keeps TMPDIR's other value, GOCACHE,
// GOFLAGS, and GOPROXY as they are, and adds none of the bench's (a shared
// TMPDIR and build cache, -count=1, no Go network), so the failures they
// hide show: a fish shell's syntax, a build cache the sandbox does not let
// commands write, a temporary directory a read-only subagent cannot write.
// It still drops the variables that would steer a harness away from its
// defaults or reach the launching session (UAH_*, OPENAI_*, CLAUDE*,
// web-tty's).
func OwnerEnv(environ []string, shell, tmp string) []string {
	var env []string
	for _, kv := range environ {
		name, _, _ := strings.Cut(kv, "=")
		if ownerDrop(name) || name == "SHELL" || (tmp != "" && name == "TMPDIR") {
			continue
		}
		env = append(env, kv)
	}
	env = append(env, "SHELL="+shell, "PYTHONDONTWRITEBYTECODE=1", "NO_COLOR=1")
	if tmp != "" {
		env = append(env, "TMPDIR="+tmp)
	}

	return env
}

func ownerDrop(name string) bool {
	for _, p := range []string{"UAH_", "WEBTTY_", "OPENAI_", "CLAUDE"} {
		if strings.HasPrefix(name, p) {
			return true
		}
	}

	return name == "NO_COLOR" || name == "PYTHONDONTWRITEBYTECODE"
}

// LoginShell is the user's login shell from the user database (dscl on
// macOS, getent elsewhere), else $SHELL: the shell a terminal opens, which
// the process that starts the bench may not have (a script under zsh).
func LoginShell(ctx context.Context) (string, error) {
	u, err := user.Current()
	if err != nil {
		return "", err
	}
	var out []byte
	if runtime.GOOS == "darwin" {
		out, err = exec.CommandContext(ctx, "dscl", ".", "-read", u.HomeDir, "UserShell").Output()
	} else {
		out, err = exec.CommandContext(ctx, "getent", "passwd", u.Username).Output()
	}
	if err == nil {
		if s := ParseLoginShell(string(out)); s != "" {
			return s, nil
		}
	}
	if s := os.Getenv("SHELL"); s != "" {
		return s, nil
	}

	return "", errors.Join(errors.New("no login shell found: pass -shell"), err)
}

// ParseLoginShell reads the shell from dscl's "UserShell: /bin/zsh" or a
// passwd line (its seventh field).
func ParseLoginShell(out string) string {
	out = strings.TrimSpace(out)
	if rest, ok := strings.CutPrefix(out, "UserShell:"); ok {
		return strings.TrimSpace(rest)
	}
	if f := strings.Split(out, ":"); len(f) == 7 {
		return strings.TrimSpace(f[6])
	}

	return ""
}

// UserTempDir is the per-user temporary directory a login session has
// (macOS's DARWIN_USER_TEMP_DIR), or "" where there is none, so a bench
// started from a sandboxed tool's TMPDIR still gives the harness the
// owner's.
func UserTempDir(ctx context.Context) string {
	if runtime.GOOS != "darwin" {
		return ""
	}
	out, err := exec.CommandContext(ctx, "getconf", "DARWIN_USER_TEMP_DIR").Output()
	if err != nil {
		return ""
	}

	return strings.TrimSpace(string(out))
}

// checkShell refuses a shell that is not an executable file.
func checkShell(shell string) error {
	st, err := os.Stat(shell)
	if err != nil {
		return fmt.Errorf("shell: %w", err)
	}
	if st.IsDir() || st.Mode()&0o111 == 0 {
		return fmt.Errorf("shell %s is not executable", shell)
	}

	return nil
}
