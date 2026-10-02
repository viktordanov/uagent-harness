// Package filter decides which entries logq prints.
package filter

import (
	"fmt"
	"strings"
	"time"

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

// Cutoff is the value of --after: a time, or a duration before the newest
// entry.
type Cutoff struct {
	at  time.Time
	ago time.Duration
}

// ParseCutoff reads an RFC 3339 time or a positive duration.
func ParseCutoff(s string) (Cutoff, error) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return Cutoff{at: t}, nil
	}
	if d, err := time.ParseDuration(s); err == nil && d > 0 {
		return Cutoff{ago: d}, nil
	}

	return Cutoff{}, fmt.Errorf("bad time %q: want RFC 3339 (2026-03-01T10:00:00Z) or a duration (90m, 2h)", s)
}

// Filter keeps the entries at or after the cutoff; a duration counts back
// from the newest of es.
func (c Cutoff) Filter(es []entry.Entry) Filter {
	at := c.at
	if c.ago > 0 {
		var newest time.Time
		for _, e := range es {
			if e.Time.After(newest) {
				newest = e.Time
			}
		}
		at = newest.Add(-c.ago)
	}

	return Func(func(e entry.Entry) bool { return !e.Time.Before(at) })
}
