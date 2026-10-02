package filter

import (
	"testing"

	"example.com/logq/internal/entry"
)

func TestApply(t *testing.T) {
	es := []entry.Entry{
		{Level: entry.Debug, Msg: "cache miss"},
		{Level: entry.Warn, Msg: "slow query"},
		{Level: entry.Error, Msg: "query failed"},
	}
	min, err := MinLevel("warn")
	if err != nil {
		t.Fatal(err)
	}
	got := Apply(es, min, Contains("query"))
	if len(got) != 2 || got[0].Msg != "slow query" {
		t.Fatalf("got %+v", got)
	}
	if _, err := MinLevel("loud"); err == nil {
		t.Error("an unknown level is not an error")
	}
}
