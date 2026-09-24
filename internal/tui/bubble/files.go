package bubble

import (
	"context"
	"io/fs"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// maxFiles bounds the "@" file list in very large workspaces.
const maxFiles = 20000

// workspaceFiles lists the workspace's files for "@" mentions: git's tracked
// and untracked-but-not-ignored files in a repository, else a walk that skips
// hidden directories.
func workspaceFiles(ctx context.Context, dir string) []string {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "ls-files", "--cached", "--others", "--exclude-standard").Output()
	if err == nil {
		files := strings.Split(strings.TrimSpace(string(out)), "\n")
		if len(files) == 1 && files[0] == "" {
			return []string{}
		}

		return files[:min(len(files), maxFiles)]
	}
	files := []string{}
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || len(files) >= maxFiles || ctx.Err() != nil {
			return filepath.SkipAll
		}
		if d.IsDir() && path != dir && strings.HasPrefix(d.Name(), ".") {
			return filepath.SkipDir
		}
		if !d.IsDir() {
			if rel, err := filepath.Rel(dir, path); err == nil {
				files = append(files, rel)
			}
		}

		return nil
	})

	return files
}
