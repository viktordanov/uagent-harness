// Command inventoryapi is the fake inventory API the curl-local-api task
// talks to: deterministic items over five pages, and details for the
// items flagged for audit.
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
)

type item struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Qty   int    `json:"qty"`
	Audit bool   `json:"audit"`
}

type details struct {
	ID          int    `json:"id"`
	Supplier    string `json:"supplier"`
	LastCounted string `json:"last_counted"`
	Bin         string `json:"bin"`
}

var names = []string{"bolt", "nut", "washer", "hinge", "bracket", "spring", "gear", "valve", "gasket", "clamp", "rivet", "pulley"}

const pageSize = 5

// items lists 23 items in a fixed, unsorted order.
func items() []item {
	var out []item
	for i := range 23 {
		id := 100 + (i*37)%97
		out = append(out, item{ID: id, Name: fmt.Sprintf("%s-%d", names[i%len(names)], id), Qty: (id * 13) % 50, Audit: id%4 == 1})
	}

	return out
}

func main() {
	all := items()
	byID := map[int]item{}
	for _, it := range all {
		byID[it.ID] = it
	}
	pages := (len(all) + pageSize - 1) / pageSize
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/items", func(w http.ResponseWriter, r *http.Request) {
		page := 1
		if p := r.URL.Query().Get("page"); p != "" {
			n, err := strconv.Atoi(p)
			if err != nil || n < 1 || n > pages {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "no such page"})

				return
			}
			page = n
		}
		lo := (page - 1) * pageSize
		hi := min(lo+pageSize, len(all))
		var next *int
		if page < pages {
			n := page + 1
			next = &n
		}
		writeJSON(w, http.StatusOK, map[string]any{"page": page, "items": all[lo:hi], "next": next})
	})
	mux.HandleFunc("/v1/items/", func(w http.ResponseWriter, r *http.Request) {
		rest := strings.TrimPrefix(r.URL.Path, "/v1/items/")
		idStr, ok := strings.CutSuffix(rest, "/details")
		id, err := strconv.Atoi(idStr)
		it, found := byID[id]
		if !ok || err != nil || !found || !it.Audit {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})

			return
		}
		writeJSON(w, http.StatusOK, details{
			ID: id, Supplier: []string{"Acme Supply", "Bolt Bros", "Coastal Parts"}[id%3],
			LastCounted: fmt.Sprintf("2026-0%d-%02d", 1+id%9, 1+id%28), Bin: fmt.Sprintf("%c-%02d", 'A'+id%6, id%40),
		})
	})
	log.Fatal(http.ListenAndServe("127.0.0.1:"+os.Getenv("PORT"), mux))
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
