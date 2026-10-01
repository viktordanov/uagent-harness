package pipeline

import "strings"

// regionAliases maps legacy spellings to region codes.
var regionAliases = map[string]string{
	"emea": "eu", "europe": "eu", "us": "na", "usa": "na", "north-america": "na", "apj": "ap",
}

// normalizeRegion trims and lower-cases a region and resolves aliases. An
// empty region stays empty: "unassigned" is a valid state.
func normalizeRegion(r string) string {
	r = strings.ToLower(strings.TrimSpace(r))
	if alias, ok := regionAliases[r]; ok {
		return alias
	}

	return r
}

func normalizeAll(in []Record) []Record {
	out := make([]Record, len(in))
	for i, r := range in {
		r.ID = strings.TrimSpace(r.ID)
		r.Region = normalizeRegion(r.Region)
		out[i] = r
	}

	return out
}
