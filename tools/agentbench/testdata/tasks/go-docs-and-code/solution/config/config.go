// Package config loads the fetcher's settings: defaults, then the config
// file, then environment variables.
package config

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// Config is the fetcher's settings.
type Config struct {
	Timeout   time.Duration
	UserAgent string
	// Retries is how many times a failed fetch is tried again.
	Retries int
}

// Default is the configuration without a file or environment.
func Default() Config {
	return Config{Timeout: 30 * time.Second, UserAgent: "fetcher/1.0", Retries: 3}
}

// envNames maps each key to its environment variable.
var envNames = map[string]string{
	"timeout":    "FETCHER_TIMEOUT",
	"user_agent": "FETCHER_USER_AGENT",
	"retries":    "FETCHER_RETRIES",
}

// Load reads the config file r (nil for none) over the defaults, then the
// environment through getenv.
func Load(r io.Reader, getenv func(string) string) (Config, error) {
	c := Default()
	if r != nil {
		sc := bufio.NewScanner(r)
		for n := 1; sc.Scan(); n++ {
			line, _, _ := strings.Cut(sc.Text(), "#")
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			key, value, ok := strings.Cut(line, "=")
			if !ok {
				return c, fmt.Errorf("line %d: want key = value", n)
			}
			if err := c.set(strings.TrimSpace(key), strings.TrimSpace(value)); err != nil {
				return c, fmt.Errorf("line %d: %w", n, err)
			}
		}
		if err := sc.Err(); err != nil {
			return c, err
		}
	}
	for key, env := range envNames {
		if v := getenv(env); v != "" {
			if err := c.set(key, v); err != nil {
				return c, fmt.Errorf("%s: %w", env, err)
			}
		}
	}

	return c, nil
}

func (c *Config) set(key, value string) error {
	switch key {
	case "timeout":
		d, err := time.ParseDuration(value)
		if err != nil {
			return fmt.Errorf("timeout: %w", err)
		}
		c.Timeout = d
	case "user_agent":
		c.UserAgent = value
	case "retries":
		n, err := strconv.Atoi(value)
		if err != nil || n < 0 {
			return fmt.Errorf("retries: want a whole number of 0 or more, got %q", value)
		}
		c.Retries = n
	default:
		return fmt.Errorf("unknown key %q", key)
	}

	return nil
}
