package config

import (
	"fmt"
	"strconv"
	"time"
)

func applyEnv(c *Config, getenv func(string) string) error {
	if v := getenv("APP_ADDR"); v != "" {
		c.Addr = v
	}
	if v := getenv("APP_TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return fmt.Errorf("env: APP_TIMEOUT: %w", err)
		}
		c.Timeout = d
	}
	if v := getenv("APP_DEBUG"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("env: APP_DEBUG: %w", err)
		}
		c.Debug = b
	}

	return nil
}
