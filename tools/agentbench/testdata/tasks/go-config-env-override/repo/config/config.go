// Package config loads the app's settings from defaults, a file, the
// environment, and flags; see README.md.
package config

import "time"

// Config is the app's settings.
type Config struct {
	Addr    string
	Timeout time.Duration
	Debug   bool
}

// Defaults returns the default settings.
func Defaults() Config {
	return Config{Addr: ":8080", Timeout: 30 * time.Second}
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
	if err := applyFlags(&c, args); err != nil {
		return c, err
	}
	if err := applyEnv(&c, getenv); err != nil {
		return c, err
	}

	return c, nil
}
