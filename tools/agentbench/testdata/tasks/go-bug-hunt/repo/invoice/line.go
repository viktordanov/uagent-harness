package invoice

// Line is one product on an order.
type Line struct {
	SKU      string
	Quantity int
	Unit     Cents
}

// Subtotal is the line's price before discounts and tax.
func (l Line) Subtotal() Cents { return l.Unit * Cents(l.Quantity) }
