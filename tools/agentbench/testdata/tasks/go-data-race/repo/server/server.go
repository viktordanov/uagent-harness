// Package server serves requests and counts them.
package server

import (
	"fmt"
	"net/http"
	"sort"
	"strings"

	"example.com/statsrv/stats"
)

// Handler counts each request by path and serves /stats.
func Handler(c *stats.Counter) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Inc(r.URL.Path)
		if r.URL.Path != "/stats" {
			fmt.Fprintln(w, "ok")

			return
		}
		snap := c.Snapshot()
		keys := make([]string, 0, len(snap))
		for k := range snap {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var b strings.Builder
		for _, k := range keys {
			fmt.Fprintf(&b, "%s %d\n", k, snap[k])
		}
		fmt.Fprint(w, b.String())
	})
}
