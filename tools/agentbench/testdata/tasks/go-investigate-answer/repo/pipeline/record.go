// Package pipeline cleans, enriches, and routes sales records.
package pipeline

// Record is one sale.
type Record struct {
	ID     string
	Region string // optional: an empty region means "unassigned"
	Amount int64  // cents
	Test   bool   // a synthetic record from monitoring
	Tax    int64  // filled by enrich
	Shard  int    // filled by route
}

// Result is a batch's output: records grouped by region, in shard order.
type Result struct {
	Groups   map[string][]Record
	Rejected []Rejection
}

// Rejection is a record validate refused, with the reason.
type Rejection struct {
	ID     string
	Reason string
}
