package format

import (
	"encoding/json"
	"io"

	"example.com/inventory/internal/store"
)

type jsonItem struct {
	Name  string  `json:"name"`
	Qty   int     `json:"qty"`
	Price float64 `json:"price"`
}

// JSON writes the items as a JSON array.
func JSON(w io.Writer, items []store.Item) error {
	out := make([]jsonItem, 0, len(items))
	for _, it := range items {
		out = append(out, jsonItem{Name: it.Name, Qty: it.Qty, Price: float64(it.PriceCents) / 100})
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")

	return enc.Encode(out)
}
