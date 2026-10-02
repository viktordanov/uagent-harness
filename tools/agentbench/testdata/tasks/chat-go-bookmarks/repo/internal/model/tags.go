package model

import (
	"fmt"
	"strings"
	"unicode"
)

// MaxTagLen is the longest tag allowed, in bytes.
const MaxTagLen = 32

// ValidateTag reports whether t is a usable tag: not empty, not too long,
// and made of letters, digits, '-' and '_' only.
func ValidateTag(t string) error {
	if t == "" {
		return fmt.Errorf("%w: empty tag", ErrInvalid)
	}
	if len(t) > MaxTagLen {
		return fmt.Errorf("%w: tag %q is longer than %d bytes", ErrInvalid, t, MaxTagLen)
	}
	for _, r := range t {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '-' && r != '_' {
			return fmt.Errorf("%w: tag %q has a bad character %q", ErrInvalid, t, r)
		}
	}
	return nil
}

// NormalizeTags trims each tag, drops empty ones, and removes duplicates
// that differ only in case, keeping the first spelling seen. The order of
// the remaining tags is kept. It never returns nil.
func NormalizeTags(tags []string) []string {
	out := make([]string, 0, len(tags))
	seen := make(map[string]bool, len(tags))
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		key := strings.ToLower(t)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, t)
	}
	return out
}
