// Package store reads inventory files.
package store

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Item is one inventory line.
type Item struct {
	Name       string
	Qty        int
	PriceCents int64
}

// Load reads lines of name,qty,price; blank lines and lines starting with
// # are skipped.
func Load(r io.Reader) ([]Item, error) {
	var items []Item
	sc := bufio.NewScanner(r)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, ",")
		if len(f) != 3 {
			return nil, fmt.Errorf("line %d: want name,qty,price", n)
		}
		qty, err := strconv.Atoi(strings.TrimSpace(f[1]))
		if err != nil {
			return nil, fmt.Errorf("line %d: bad qty: %w", n, err)
		}
		price, err := parseCents(strings.TrimSpace(f[2]))
		if err != nil {
			return nil, fmt.Errorf("line %d: bad price: %w", n, err)
		}
		items = append(items, Item{Name: strings.TrimSpace(f[0]), Qty: qty, PriceCents: price})
	}

	return items, sc.Err()
}

func parseCents(s string) (int64, error) {
	whole, frac, _ := strings.Cut(s, ".")
	if len(frac) > 2 {
		return 0, fmt.Errorf("%q has more than two decimals", s)
	}
	frac += strings.Repeat("0", 2-len(frac))
	w, err := strconv.ParseInt(whole, 10, 64)
	if err != nil {
		return 0, err
	}
	f, err := strconv.ParseInt(frac, 10, 64)
	if err != nil {
		return 0, err
	}

	return w*100 + f, nil
}
