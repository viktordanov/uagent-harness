// Command batch runs a sample batch and prints the group sizes.
package main

import (
	"fmt"
	"sort"

	"example.com/batch/pipeline"
)

func main() {
	res := pipeline.ProcessBatch([]pipeline.Record{
		{ID: "a1", Region: "Europe", Amount: 1200},
		{ID: "a2", Region: "", Amount: 800},
		{ID: "a3", Region: "USA", Amount: 300},
	})
	var keys []string
	for k := range res.Groups {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Printf("%s: %d\n", k, len(res.Groups[k]))
	}
}
