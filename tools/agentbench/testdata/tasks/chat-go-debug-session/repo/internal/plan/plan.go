// Package plan is the catalog of subscription plans.
package plan

import (
	"fmt"
	"sort"

	"example.com/billing/internal/money"
)

// Plan is a subscription plan billed per calendar month.
type Plan struct {
	ID      string
	Name    string
	Monthly money.Cents
}

// Catalog maps plan ids to plans.
type Catalog map[string]Plan

// Default is the catalog the CLI uses.
var Default = Catalog{
	"starter": {ID: "starter", Name: "Starter", Monthly: 900},
	"pro":     {ID: "pro", Name: "Pro", Monthly: 3100},
	"team":    {ID: "team", Name: "Team", Monthly: 12000},
}

// Lookup returns the plan with the given id.
func (c Catalog) Lookup(id string) (Plan, error) {
	p, ok := c[id]
	if !ok {
		return Plan{}, fmt.Errorf("plan: unknown plan %q", id)
	}
	return p, nil
}

// IDs returns the plan ids in sorted order.
func (c Catalog) IDs() []string {
	ids := make([]string, 0, len(c))
	for id := range c {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
