package config

import (
	"os"
	"path/filepath"

	"github.com/viktordanov/uagent-harness/internal/home"
)

// legacyProjectName is the project directory uah read before it moved to .uah.
const legacyProjectName = ".uagent"

// ProjectMove is the command that moves a workspace's old .uagent directory
// to .uah, which uah reads instead: `git mv .uagent .uah` in a repository,
// else `mv .uagent .uah`. It returns "" when there is nothing to move. uah
// never moves repository files itself.
func ProjectMove(workspace string) string {
	if !isDir(filepath.Join(workspace, legacyProjectName)) || exists(ProjectDir(workspace)) {
		return ""
	}
	move := "mv"
	if inGitRepo(workspace) {
		move = "git mv"
	}

	return move + " " + legacyProjectName + " " + home.Name
}

// ProjectMoveNotice says what ProjectMove does and why, or returns "".
func ProjectMoveNotice(workspace string) string {
	move := ProjectMove(workspace)
	if move == "" {
		return ""
	}

	return filepath.Join(workspace, legacyProjectName) + " is no longer read; uah reads project files from " + home.Name +
		" now: run `" + move + "` in " + workspace
}

// inGitRepo reports whether dir or a parent has .git.
func inGitRepo(dir string) bool {
	for {
		if exists(filepath.Join(dir, ".git")) {
			return true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return false
		}
		dir = parent
	}
}

func exists(path string) bool {
	_, err := os.Lstat(path)

	return err == nil
}

func isDir(path string) bool {
	info, err := os.Stat(path)

	return err == nil && info.IsDir()
}
