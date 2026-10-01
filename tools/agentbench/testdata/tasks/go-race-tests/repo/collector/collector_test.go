package collector

import (
	"fmt"
	"testing"
)

func TestCollectAll(t *testing.T) {
	producers := map[string]func() []int{}
	for i := 0; i < 16; i++ {
		producers[fmt.Sprintf("p%d", i)] = func() []int {
			vals := make([]int, 500)
			for j := range vals {
				vals[j] = j
			}

			return vals
		}
	}
	got := Collect(producers)
	if len(got) != 16*500 {
		t.Fatalf("got %d results, want %d", len(got), 16*500)
	}
}
