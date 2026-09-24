package sandbox

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Shell returns a shell for the runner that runs every command under the
// policy and with the environment policy: a small script in dir that execs
// the sandbox around realShell, so `<script> -c <command>` is
// `<sandbox> <realShell> -c <command>`. Scripts are named by their content,
// so a session reuses one and a changed policy gets a new one. With
// FullAccess and the default environment policy it returns realShell.
func Shell(dir string, p Policy, env EnvPolicy, realShell string) (string, error) {
	argv := []string{realShell}
	if p.Mode != FullAccess {
		var err error
		if argv, err = p.Wrap(argv); err != nil {
			return "", err
		}
	} else if env.isDefault() {
		return realShell, nil
	}
	words := make([]string, 0, len(argv)+8)
	if !env.isDefault() {
		words = append(words, envWords(env)...)
	}
	for _, a := range argv {
		words = append(words, quote(a))
	}
	script := "#!/bin/sh\n# Written by uah: runs the command in the " + string(p.Mode) + " sandbox.\nexec " + strings.Join(words, " ") + " \"$@\"\n"

	return WriteScript(dir, script)
}

// WriteScript writes an executable script into dir, named by its content
// (sh-<hash>), unless it is there already, and returns its path.
func WriteScript(dir, script string) (string, error) {
	sum := sha256.Sum256([]byte(script))
	path := filepath.Join(dir, "sh-"+hex.EncodeToString(sum[:8]))
	if _, err := os.Stat(path); err == nil {
		return path, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("failed to check the sandbox shell: %w", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("failed to create %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".sh-*")
	if err != nil {
		return "", fmt.Errorf("failed to write the sandbox shell: %w", err)
	}
	_, werr := tmp.WriteString(script)
	if err := errors.Join(werr, tmp.Chmod(0o700), tmp.Close()); err != nil {
		_ = os.Remove(tmp.Name())

		return "", fmt.Errorf("failed to write the sandbox shell: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return "", fmt.Errorf("failed to save the sandbox shell: %w", err)
	}

	return path, nil
}

// Quote quotes s for /bin/sh.
func Quote(s string) string { return quote(s) }

// envWords starts the command with `env -i` and the variables the policy
// keeps. Inherited variables are copied from the environment when the
// command runs ("NAME=$NAME"), so no inherited value is written to disk;
// only the policy's own Set values are.
func envWords(env EnvPolicy) []string {
	words := []string{"/usr/bin/env", "-i"}
	for _, kv := range env.Apply(os.Environ()) {
		name, value, _ := strings.Cut(kv, "=")
		if !shellName(name) {
			continue
		}
		if v, ok := env.Set[name]; ok && v == value {
			words = append(words, quote(name+"="+value))
		} else {
			words = append(words, name+`="$`+name+`"`)
		}
	}

	return words
}

// shellName reports whether s can be referenced as $s.
func shellName(s string) bool {
	for i, r := range s {
		if r != '_' && (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') && (i == 0 || r < '0' || r > '9') {
			return false
		}
	}

	return s != ""
}

// quote quotes s for /bin/sh.
func quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
