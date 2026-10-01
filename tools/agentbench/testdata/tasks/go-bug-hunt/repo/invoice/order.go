package invoice

// Order is a customer's order.
type Order struct {
	Lines  []Line
	Code   string // a discount code, or ""
	Region string
}

// Totals is an invoice's money.
type Totals struct {
	Subtotal   Cents
	Discounted Cents
	Tax        Cents
	Total      Cents
}

// Price computes the order's totals: the subtotal, the subtotal after the
// discount code, the tax on that, and the total.
func Price(o Order) Totals {
	var t Totals
	for _, l := range o.Lines {
		t.Subtotal += l.Subtotal()
	}
	t.Discounted = t.Subtotal
	if c, ok := LookupCode(o.Code); ok {
		t.Discounted = applyDiscount(t.Subtotal, c)
	}
	t.Tax = tax(t.Discounted, o.Region)
	t.Total = t.Discounted + t.Tax

	return t
}
