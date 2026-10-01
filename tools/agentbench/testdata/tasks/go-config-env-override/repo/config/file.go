package config

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

func applyFile(c *Config, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			return fmt.Errorf("file: bad line %q", line)
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		switch k {
		case "addr":
			c.Addr = v
		case "timeout":
			d, err := time.ParseDuration(v)
			if err != nil {
				return fmt.Errorf("file: timeout: %w", err)
			}
			c.Timeout = d
		case "debug":
			b, err := strconv.ParseBool(v)
			if err != nil {
				return fmt.Errorf("file: debug: %w", err)
			}
			c.Debug = b
		default:
			return fmt.Errorf("file: unknown key %q", k)
		}
	}

	return sc.Err()
}
