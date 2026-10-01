package invoice

import "strings"

// Code is a discount code: a percentage off the order's subtotal.
type Code struct {
	Name    string
	Percent float64 // e.g. 15 for 15%, 12.5 for 12.5%
}

// knownCodes are the codes customers can enter.
var knownCodes = map[string]Code{
	"WELCOME10": {"WELCOME10", 10},
	"SPRING15":  {"SPRING15", 15},
	"VIP12":     {"VIP12", 12.5},
}

// LookupCode finds a code, ignoring case and spaces.
func LookupCode(s string) (Code, bool) {
	c, ok := knownCodes[strings.ToUpper(strings.TrimSpace(s))]

	return c, ok
}
