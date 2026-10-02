// Package money holds amounts in integer cents.
package money

import (
	"fmt"
	"strconv"
	"strings"
)

// Cents is an amount of money in the smallest unit of its currency.
type Cents int64

// Prorate returns c * num / den, rounded half away from zero. den must be
// positive.
func (c Cents) Prorate(num, den int64) Cents {
	if den <= 0 {
		panic("money: Prorate with a non-positive denominator")
	}
	n := int64(c) * num
	q, r := n/den, n%den
	if r < 0 {
		r = -r
	}
	if 2*r >= den {
		if n < 0 {
			q--
		} else {
			q++
		}
	}
	return Cents(q)
}

// Percent returns p percent of c, rounded half away from zero.
func (c Cents) Percent(p int64) Cents { return c.Prorate(p, 100) }

// String formats c as "12.34", with a leading "-" when negative.
func (c Cents) String() string {
	sign := ""
	v := int64(c)
	if v < 0 {
		sign = "-"
		v = -v
	}
	return fmt.Sprintf("%s%d.%02d", sign, v/100, v%100)
}

// Parse reads an amount written as "12", "12.3" or "12.34".
func Parse(s string) (Cents, error) {
	s = strings.TrimSpace(s)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	whole, frac, hasFrac := strings.Cut(s, ".")
	if whole == "" || (hasFrac && (frac == "" || len(frac) > 2)) {
		return 0, fmt.Errorf("money: bad amount %q", s)
	}
	w, err := strconv.ParseInt(whole, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("money: bad amount %q", s)
	}
	var f int64
	if hasFrac {
		if len(frac) == 1 {
			frac += "0"
		}
		f, err = strconv.ParseInt(frac, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("money: bad amount %q", s)
		}
	}
	v := Cents(w*100 + f)
	if neg {
		v = -v
	}
	return v, nil
}
