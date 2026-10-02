package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"example.com/billing/internal/customer"
	"example.com/billing/internal/dunning"
	"example.com/billing/internal/invoice"
	"example.com/billing/internal/money"
	"example.com/billing/internal/period"
	"example.com/billing/internal/plan"
)

const usage = `usage:
  billctl invoice -month YYYY-MM customers.json
  billctl overdue -month YYYY-MM -today YYYY-MM-DD customers.json
`

// errUsage marks a usage error: exit status 2.
var errUsage = errors.New("usage")

// run is the whole CLI; it returns the exit status.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	var err error
	switch args[0] {
	case "invoice":
		err = runInvoice(args[1:], stdout, stderr)
	case "overdue":
		err = runOverdue(args[1:], stdout, stderr)
	default:
		err = fmt.Errorf("%w: unknown command %q", errUsage, args[0])
	}
	switch {
	case err == nil:
		return 0
	case errors.Is(err, errUsage):
		fmt.Fprintf(stderr, "billctl: %v\n%s", err, usage)
		return 2
	default:
		fmt.Fprintf(stderr, "billctl: %v\n", err)
		return 1
	}
}

// common parses the flags and the customer file shared by both commands.
type common struct {
	fs        *flag.FlagSet
	month     string
	customers []customer.Customer
	year      int
	mon       time.Month
}

func newCommon(name string, stderr io.Writer) *common {
	c := &common{fs: flag.NewFlagSet(name, flag.ContinueOnError)}
	c.fs.SetOutput(stderr)
	c.fs.StringVar(&c.month, "month", "", "billed month, YYYY-MM")
	return c
}

func (c *common) parse(args []string) error {
	if err := c.fs.Parse(args); err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	if c.fs.NArg() != 1 {
		return fmt.Errorf("%w: want one customer file", errUsage)
	}
	m, err := period.ParseMonth(c.month, time.UTC)
	if err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	c.year, c.mon = m.Start.Year(), m.Start.Month()
	f, err := os.Open(c.fs.Arg(0))
	if err != nil {
		return err
	}
	defer f.Close()
	c.customers, err = customer.Load(f)
	return err
}

// invoices builds the month's invoice for every customer subscribed in it.
func (c *common) invoices() ([]invoice.Invoice, error) {
	var out []invoice.Invoice
	for _, cu := range c.customers {
		p, err := plan.Default.Lookup(cu.PlanID)
		if err != nil {
			return nil, fmt.Errorf("customer %s: %w", cu.ID, err)
		}
		if inv, ok := invoice.Build(cu, p, c.year, c.mon); ok {
			out = append(out, inv)
		}
	}
	return out, nil
}

func runInvoice(args []string, stdout, stderr io.Writer) error {
	c := newCommon("invoice", stderr)
	if err := c.parse(args); err != nil {
		return err
	}
	invs, err := c.invoices()
	if err != nil {
		return err
	}
	var total money.Cents
	for _, inv := range invs {
		if err := invoice.WriteText(stdout, inv); err != nil {
			return err
		}
		total += inv.Total
	}
	fmt.Fprintf(stdout, "%d invoices, %s total\n", len(invs), total)
	return nil
}

func runOverdue(args []string, stdout, stderr io.Writer) error {
	c := newCommon("overdue", stderr)
	today := c.fs.String("today", "", "the date to assess at, YYYY-MM-DD, in each customer's time zone")
	if err := c.parse(args); err != nil {
		return err
	}
	if _, err := time.Parse("2006-01-02", *today); err != nil {
		return fmt.Errorf("%w: -today: want YYYY-MM-DD", errUsage)
	}
	invs, err := c.invoices()
	if err != nil {
		return err
	}
	byID := make(map[string]customer.Customer, len(c.customers))
	for _, cu := range c.customers {
		byID[cu.ID] = cu
	}
	for _, inv := range invs {
		loc := byID[inv.CustomerID].Location
		now, _ := time.ParseInLocation("2006-01-02", *today, loc)
		n := dunning.Assess(inv, now)
		if n.Stage == dunning.NotDue {
			continue
		}
		fmt.Fprintf(stdout, "%s\t%d days\t%s\t%s\n", n.Invoice, n.DaysOverdue, n.Stage, n.Fee)
	}
	return nil
}
