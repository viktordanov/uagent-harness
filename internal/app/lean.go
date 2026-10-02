package app

import (
	"fmt"

	"github.com/viktordanov/uah/internal/config"
)

// Lean mode's values (lean): how many effort levels below the user's a
// follow-up turn goes; any other than off also primes a new session with
// the workspace's context.
const (
	LeanOff      = "off"
	LeanOneStep  = "1-step"
	LeanTwoSteps = "2-steps"
)

// pickLean checks lean: off by default.
func pickLean(cfg config.Config) (string, error) {
	switch cfg.Lean {
	case "", LeanOff:
		return LeanOff, nil
	case LeanOneStep, LeanTwoSteps:
		return cfg.Lean, nil
	}

	return "", usage(fmt.Errorf("invalid lean %q (want off, 1-step, or 2-steps)", cfg.Lean))
}

// LeanSteps is the number of effort levels a follow-up turn goes down: 0
// when Lean mode is off.
func LeanSteps(lean string) int {
	switch lean {
	case LeanOneStep:
		return 1
	case LeanTwoSteps:
		return 2
	}

	return 0
}
