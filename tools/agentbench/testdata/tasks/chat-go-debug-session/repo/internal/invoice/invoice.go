// Package invoice builds monthly invoices, prorated by the day for customers
// who start or stop during the month.
package invoice

import (
	"fmt"
	"time"

	"example.com/billing/internal/customer"
	"example.com/billing/internal/money"
	"example.com/billing/internal/period"
	"example.com/billing/internal/plan"
)

// PaymentTerms is how long after the end of the billed month an invoice is
// due, in days.
const PaymentTerms = 14

// Line is one charge on an invoice.
type Line struct {
	Description string
	Period      period.Period
	Days        int
	Amount      money.Cents
}

// Invoice is what a customer owes for one month.
type Invoice struct {
	Number     string
	CustomerID string
	Month      period.Period
	Due        time.Time
	Lines      []Line
	Total      money.Cents
}

// Build returns the invoice of customer c on plan p for the given month,
// taken in the customer's time zone. It returns false when the customer was
// not subscribed during the month.
//
// A partial month is billed for the days the customer was subscribed:
// Monthly * activeDays / daysInMonth, rounded to the cent.
func Build(c customer.Customer, p plan.Plan, year int, month time.Month) (Invoice, bool) {
	loc := c.Location
	if loc == nil {
		loc = time.UTC
	}
	m := period.Month(year, month, loc)
	active, ok := c.Active(m)
	if !ok {
		return Invoice{}, false
	}
	monthDays := m.Days()
	days := active.Days()
	line := Line{
		Description: fmt.Sprintf("%s plan", p.Name),
		Period:      active,
		Days:        days,
		Amount:      p.Monthly,
	}
	if days < monthDays {
		line.Description = fmt.Sprintf("%s plan, %d of %d days", p.Name, days, monthDays)
		line.Amount = p.Monthly.Prorate(int64(days), int64(monthDays))
	}
	inv := Invoice{
		Number:     Number(c.ID, year, month),
		CustomerID: c.ID,
		Month:      m,
		Due:        m.End.AddDate(0, 0, PaymentTerms),
		Lines:      []Line{line},
	}
	for _, l := range inv.Lines {
		inv.Total += l.Amount
	}
	return inv, true
}

// Number is the invoice number of a customer's month: "INV-2026-03-c1".
func Number(customerID string, year int, month time.Month) string {
	return fmt.Sprintf("INV-%04d-%02d-%s", year, int(month), customerID)
}
