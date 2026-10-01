// Package money formats amounts held in minor units (cents).
package money

import (
	"fmt"
	"strings"
)

//go:generate ../build.sh

type currency struct {
	decimals int
	name     string
}

// Name returns a currency's English name.
func Name(code string) (string, error) {
	c, ok := currencies[code]
	if !ok {
		return "", fmt.Errorf("unknown currency %q", code)
	}

	return c.name, nil
}

// Format writes an amount in minor units with the currency's decimals,
// e.g. Format("USD", -1234) is "-12.34 USD".
func Format(code string, minor int64) (string, error) {
	c, ok := currencies[code]
	if !ok {
		return "", fmt.Errorf("unknown currency %q", code)
	}
	sign := ""
	if minor < 0 {
		sign, minor = "-", -minor
	}
	unit := int64(1)
	for range c.decimals {
		unit *= 10
	}
	frac := fmt.Sprintf("%0*d", c.decimals, minor%unit)

	return fmt.Sprintf("%s%d.%s %s", sign, minor/unit, strings.TrimSpace(frac), code), nil
}
