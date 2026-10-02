// Package customer loads the customer list the billing run works from.
package customer

import (
	"encoding/json"
	"fmt"
	"io"
	"time"
	_ "time/tzdata" // so time zones load in minimal containers too

	"example.com/billing/internal/period"
)

// Customer is one subscriber. Start and End are dates in the customer's
// own time zone; End is zero while the subscription runs.
type Customer struct {
	ID       string
	Name     string
	PlanID   string
	Location *time.Location
	Start    time.Time
	End      time.Time
}

// Active returns the part of the subscription inside p, and false when the
// customer was not subscribed at any time in p.
func (c Customer) Active(p period.Period) (period.Period, bool) {
	sub := period.Period{Start: c.Start, End: c.End}
	if c.End.IsZero() {
		sub.End = p.End
	}
	return p.Overlap(sub)
}

// record is a customer as written in the JSON file.
type record struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Plan     string `json:"plan"`
	Timezone string `json:"timezone"`
	Start    string `json:"start"`
	End      string `json:"end,omitempty"`
}

// Load reads a JSON array of customers. Dates are "YYYY-MM-DD" in the
// customer's time zone; an empty timezone means UTC.
func Load(r io.Reader) ([]Customer, error) {
	var recs []record
	if err := json.NewDecoder(r).Decode(&recs); err != nil {
		return nil, fmt.Errorf("customer: decode: %w", err)
	}
	out := make([]Customer, 0, len(recs))
	for i, rec := range recs {
		c, err := rec.customer()
		if err != nil {
			return nil, fmt.Errorf("customer %d (%s): %w", i, rec.ID, err)
		}
		out = append(out, c)
	}
	return out, nil
}

func (rec record) customer() (Customer, error) {
	if rec.ID == "" {
		return Customer{}, fmt.Errorf("missing id")
	}
	loc := time.UTC
	if rec.Timezone != "" {
		l, err := time.LoadLocation(rec.Timezone)
		if err != nil {
			return Customer{}, fmt.Errorf("timezone: %w", err)
		}
		loc = l
	}
	start, err := time.ParseInLocation("2006-01-02", rec.Start, loc)
	if err != nil {
		return Customer{}, fmt.Errorf("start: %w", err)
	}
	c := Customer{ID: rec.ID, Name: rec.Name, PlanID: rec.Plan, Location: loc, Start: start}
	if rec.End != "" {
		end, err := time.ParseInLocation("2006-01-02", rec.End, loc)
		if err != nil {
			return Customer{}, fmt.Errorf("end: %w", err)
		}
		if !end.After(start) {
			return Customer{}, fmt.Errorf("end %s is not after start %s", rec.End, rec.Start)
		}
		c.End = end
	}
	return c, nil
}
