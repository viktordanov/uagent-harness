// Package money formats amounts held in minor units (cents).
package money

import "fmt"

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
	if c.decimals == 0 {
		return fmt.Sprintf("%s%d %s", sign, minor, code), nil
	}

	return fmt.Sprintf("%s%d.%0*d %s", sign, minor/unit, c.decimals, minor%unit, code), nil
}
