package config

import (
	"flag"
	"fmt"
	"io"
)

func applyFlags(c *Config, args []string) error {
	fs := flag.NewFlagSet("app", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&c.Addr, "addr", c.Addr, "listen address")
	fs.DurationVar(&c.Timeout, "timeout", c.Timeout, "request timeout")
	fs.BoolVar(&c.Debug, "debug", c.Debug, "debug logging")
	fs.IntVar(&c.MaxConns, "max-conns", c.MaxConns, "maximum connections")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("flags: %w", err)
	}
	if err := checkMaxConns("flags", "-max-conns", c.MaxConns); err != nil {
		return err
	}

	return nil
}
