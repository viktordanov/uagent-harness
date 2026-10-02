// Package seed fills a store with a few demo bookmarks.
package seed

import (
	"fmt"

	"example.com/bookmarks/internal/model"
	"example.com/bookmarks/internal/store"
)

// Demo is the list Load adds.
var Demo = []model.Bookmark{
	{URL: "https://go.dev", Title: "The Go Programming Language", Tags: []string{"go", "lang"}},
	{URL: "https://pkg.go.dev/net/http", Title: "net/http package docs", Tags: []string{"Go", "http", "docs"}},
	{URL: "https://developer.mozilla.org/en-US/docs/Web/HTTP", Title: "HTTP on MDN", Tags: []string{"http", "docs"}},
	{URL: "https://www.rfc-editor.org/rfc/rfc9110", Title: "RFC 9110: HTTP Semantics", Tags: []string{"HTTP", "rfc"}},
}

// Load adds the demo bookmarks to s.
func Load(s *store.Store) error {
	for _, b := range Demo {
		if _, err := s.Create(b); err != nil {
			return fmt.Errorf("seed %s: %w", b.URL, err)
		}
	}
	return nil
}
