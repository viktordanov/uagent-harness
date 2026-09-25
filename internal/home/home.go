// Package home is uah's one home, as Codex has ~/.codex and Claude Code has
// ~/.claude: the configuration, the instructions, hook trust, MCP
// credentials, sessions, run records, the index, images, the model cache,
// and logs all live in ~/.uah, or in $UAH_HOME when it is set.
package home

import (
	"os"
	"path/filepath"
)

// Environment variables that place uah's files.
const (
	// Env overrides the home directory.
	Env = "UAH_HOME"
	// EnvConfig overrides the user configuration file (--config).
	EnvConfig = "UAH_CONFIG"
	// EnvStateDir overrides where sessions and run records live (--state-dir).
	EnvStateDir = "UAH_STATE_DIR"
)

// Name is the home directory's name in the user's home directory, and the
// project directory's name in a workspace.
const Name = ".uah"

// Dir is $UAH_HOME, else ~/.uah (./.uah when there is no home directory).
func Dir() string {
	if dir := os.Getenv(Env); dir != "" {
		return filepath.Clean(dir)
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		return Name
	}

	return filepath.Join(userHome, Name)
}

// Legacy variables that uah no longer reads, with the variable that replaced
// each one.
var Legacy = []struct{ Old, New string }{
	{"UAGENT_CONFIG", EnvConfig},
	{"UAGENT_STATE_DIR", EnvStateDir},
}

// Warnings name each legacy variable that is set and the variable to use
// instead, one line each.
func Warnings(getenv func(string) string) []string {
	var out []string
	for _, v := range Legacy {
		if getenv(v.Old) != "" {
			out = append(out, v.Old+" is no longer read; set "+v.New+" instead")
		}
	}

	return out
}
