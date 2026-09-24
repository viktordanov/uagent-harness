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
// policy: a small script in dir that execs the sandbox around realShell, so
// `<script> -c <command>` is `<sandbox> <realShell> -c <command>`. Scripts
// are named by their content, so a session reuses one and a changed policy
// gets a new one. FullAccess returns realShell.
func Shell(dir string, p Policy, realShell string) (string, error) {
	if p.Mode == FullAccess {
		return realShell, nil
	}
	argv, err := p.Wrap([]string{realShell})
	if err != nil {
		return "", err
	}
	quoted := make([]string, len(argv))
	for i, a := range argv {
		quoted[i] = quote(a)
	}
	script := "#!/bin/sh\n# Written by uah: runs the command in the " + string(p.Mode) + " sandbox.\nexec " + strings.Join(quoted, " ") + " \"$@\"\n"
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

// quote quotes s for /bin/sh.
func quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
