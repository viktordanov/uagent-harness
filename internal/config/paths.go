package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// resolvePaths makes the path keys that Codex reads as AbsolutePathBuf
// absolute: ~ and ~/ are the home directory, and a relative path is
// relative to dir, the directory of the file that set it
// (codex-rs/utils/absolute-path/src/lib.rs, resolve_path_against_base).
func resolvePaths(c *Config, dir string) error {
	path, err := resolvePath(c.ModelInstructionsFile, dir)
	if err != nil {
		return fmt.Errorf("failed to resolve model_instructions_file: %w", err)
	}
	c.ModelInstructionsFile = path

	return nil
}

func resolvePath(value, dir string) (string, error) {
	if value == "" {
		return "", nil
	}
	if value == "~" || strings.HasPrefix(value, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to find the home directory: %w", err)
		}
		value = filepath.Join(home, strings.TrimLeft(value[1:], "/"))
	}
	if filepath.IsAbs(value) {
		return filepath.Clean(value), nil
	}
	abs, err := filepath.Abs(filepath.Join(dir, value))
	if err != nil {
		return "", fmt.Errorf("failed to resolve %s: %w", value, err)
	}

	return abs, nil
}
