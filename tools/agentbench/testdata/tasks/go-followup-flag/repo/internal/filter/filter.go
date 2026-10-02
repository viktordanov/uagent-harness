// Package filter decides which entries logq prints.
package filter

import (
	"strings"

	"example.com/logq/internal/entry"
)

// A Filter keeps an entry when Keep returns true.
type Filter interface {
	Keep(e entry.Entry) bool
}

// Func is a Filter from a function.
type Func func(e entry.Entry) bool

// Keep calls f.
func (f Func) Keep(e entry.Entry) bool { return f(e) }

// MinLevel keeps entries at the named level or above.
func MinLevel(name string) (Filter, error) {
	l, err := entry.ParseLevel(name)
	if err != nil {
		return nil, err
	}

	return Func(func(e entry.Entry) bool { return e.Level >= l }), nil
}

// Contains keeps entries whose message contains s.
func Contains(s string) Filter {
	return Func(func(e entry.Entry) bool { return strings.Contains(e.Msg, s) })
}

// Apply returns the entries every filter keeps, in order.
func Apply(es []entry.Entry, fs ...Filter) []entry.Entry {
	var out []entry.Entry
next:
	for _, e := range es {
		for _, f := range fs {
			if !f.Keep(e) {
				continue next
			}
		}
		out = append(out, e)
	}

	return out
}
