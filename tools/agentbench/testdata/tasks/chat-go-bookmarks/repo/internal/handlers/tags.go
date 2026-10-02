package handlers

import (
	"net/http"
	"sort"
	"strings"
)

type tagCount struct {
	Tag   string `json:"tag"`
	Count int    `json:"count"`
}

// tags serves GET /tags: every tag in use with the number of bookmarks that
// carry it, most used first. Tags are counted ignoring case and reported in
// lower case.
func (a *API) tags(w http.ResponseWriter, r *http.Request) {
	counts := make(map[string]int)
	for _, b := range a.store.List() {
		for _, t := range b.Tags {
			counts[strings.ToLower(t)]++
		}
	}
	out := make([]tagCount, 0, len(counts))
	for t, n := range counts {
		out = append(out, tagCount{Tag: t, Count: n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Tag < out[j].Tag
	})
	writeJSON(w, http.StatusOK, out)
}
