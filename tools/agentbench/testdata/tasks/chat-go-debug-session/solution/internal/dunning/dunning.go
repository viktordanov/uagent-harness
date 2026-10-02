// Package dunning decides what to do about unpaid invoices: remind, charge
// a late fee, or suspend the account.
package dunning

import (
	"fmt"
	"time"

	"example.com/billing/internal/invoice"
	"example.com/billing/internal/money"
	"example.com/billing/internal/period"
)

// Stage is how far an unpaid invoice has gone.
type Stage int

const (
	// NotDue: the due date has not passed.
	NotDue Stage = iota
	// Reminder: overdue, a reminder is sent.
	Reminder
	// LateFee: overdue for LateFeeAfter days or more; a fee is added.
	LateFee
	// Suspend: overdue for SuspendAfter days or more; the account is
	// suspended and a larger fee is added.
	Suspend
)

func (s Stage) String() string {
	switch s {
	case NotDue:
		return "not-due"
	case Reminder:
		return "reminder"
	case LateFee:
		return "late-fee"
	case Suspend:
		return "suspend"
	}
	return fmt.Sprintf("Stage(%d)", int(s))
}

// Thresholds, in days overdue.
const (
	LateFeeAfter = 14
	SuspendAfter = 30
)

// Fee percentages of the invoice total, and the smallest fee charged.
const (
	LateFeePercent = 2
	SuspendPercent = 5
	MinFee         = money.Cents(100)
)

// Notice is the outcome for one invoice.
type Notice struct {
	Invoice     string
	DaysOverdue int
	Stage       Stage
	Fee         money.Cents
}

// DaysOverdue returns how many calendar days have passed since due, in
// due's time zone, or 0 when now is not after due.
func DaysOverdue(due, now time.Time) int {
	if !now.After(due) {
		return 0
	}
	return period.CalendarDays(due, now)
}

// Assess returns what to do about inv, unpaid at now.
func Assess(inv invoice.Invoice, now time.Time) Notice {
	n := Notice{Invoice: inv.Number}
	if !now.After(inv.Due) {
		return n
	}
	n.DaysOverdue = DaysOverdue(inv.Due, now)
	switch {
	case n.DaysOverdue >= SuspendAfter:
		n.Stage = Suspend
		n.Fee = fee(inv.Total, SuspendPercent)
	case n.DaysOverdue >= LateFeeAfter:
		n.Stage = LateFee
		n.Fee = fee(inv.Total, LateFeePercent)
	default:
		n.Stage = Reminder
	}
	return n
}

func fee(total money.Cents, percent int64) money.Cents {
	f := total.Percent(percent)
	if f < MinFee {
		f = MinFee
	}
	return f
}
