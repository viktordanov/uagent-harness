// Package size parses and formats byte sizes such as "1.5 MiB"; see
// docs/SPEC.md.
package size

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"strings"
)

// The sentinel errors that ParseSize's errors wrap.
var (
	ErrSyntax = errors.New("size: invalid syntax")
	ErrUnit   = errors.New("size: unknown unit")
	ErrRange  = errors.New("size: value out of range")
)

var units = map[string]int64{
	"": 1, "B": 1,
	"kB": 1e3, "MB": 1e6, "GB": 1e9, "TB": 1e12,
	"KiB": 1 << 10, "MiB": 1 << 20, "GiB": 1 << 30, "TiB": 1 << 40,
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// ParseSize parses a size string into bytes.
func ParseSize(s string) (int64, error) {
	s = strings.TrimSpace(s)
	i := 0
	for i < len(s) && isDigit(s[i]) {
		i++
	}
	if i == 0 {
		return 0, fmt.Errorf("%q: %w", s, ErrSyntax)
	}
	intPart, frac := s[:i], ""
	if i < len(s) && s[i] == '.' {
		j := i + 1
		for j < len(s) && isDigit(s[j]) {
			j++
		}
		if j == i+1 {
			return 0, fmt.Errorf("%q: %w", s, ErrSyntax)
		}
		frac, i = s[i+1:j], j
	}
	rest := s[i:]
	if strings.HasPrefix(rest, " ") {
		rest = rest[1:]
		if rest == "" {
			return 0, fmt.Errorf("%q: %w", s, ErrSyntax)
		}
	}
	if rest != "" && !isLetter(rest[0]) {
		return 0, fmt.Errorf("%q: %w", s, ErrSyntax)
	}
	mult, ok := units[rest]
	if !ok {
		return 0, fmt.Errorf("%q: %w", s, ErrUnit)
	}
	num, _ := new(big.Int).SetString(intPart+frac, 10)
	den := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(len(frac))), nil)
	num.Mul(num, big.NewInt(mult))
	num.Quo(num, den)
	if num.Cmp(big.NewInt(math.MaxInt64)) > 0 {
		return 0, fmt.Errorf("%q: %w", s, ErrRange)
	}

	return num.Int64(), nil
}

func isLetter(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }

// FormatSize formats n bytes for people.
func FormatSize(n int64) string {
	if n < 0 {
		return "-" + FormatSize(-n)
	}
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	names := []string{"KiB", "MiB", "GiB", "TiB"}
	u, unit := 0, int64(1024)
	for u < len(names)-1 && n >= unit*1024 {
		u++
		unit *= 1024
	}
	tenths := new(big.Int).Quo(new(big.Int).Mul(big.NewInt(n), big.NewInt(10)), big.NewInt(unit)).Int64()
	if tenths%10 == 0 {
		return fmt.Sprintf("%d %s", tenths/10, names[u])
	}

	return fmt.Sprintf("%d.%d %s", tenths/10, tenths%10, names[u])
}
