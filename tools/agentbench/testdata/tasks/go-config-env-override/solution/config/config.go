// Package config loads the app's settings from defaults, a file, the
// environment, and flags; see README.md.
package config

import (
	"fmt"
	"time"
)

// Config is the app's settings.
type Config struct {
	Addr     string
	Timeout  time.Duration
	Debug    bool
	MaxConns int
}

// Defaults returns the default settings.
func Defaults() Config {
	return Config{Addr: ":8080", Timeout: 30 * time.Second, MaxConns: 10}
}

// Load builds the settings: defaults, then the file at path (none when
// path is empty), then the environment (read through getenv), then args.
func Load(path string, getenv func(string) string, args []string) (Config, error) {
	c := Defaults()
	if path != "" {
		if err := applyFile(&c, path); err != nil {
			return c, err
		}
	}
	if err := applyEnv(&c, getenv); err != nil {
		return c, err
	}
	if err := applyFlags(&c, args); err != nil {
		return c, err
	}

	return c, nil
}

func checkMaxConns(layer, key string, n int) error {
	if n < 1 {
		return fmt.Errorf("%s: %s must be at least 1, got %d", layer, key, n)
	}

	return nil
}
