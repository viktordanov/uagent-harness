package invoice

// Catalog holds the unit price of each SKU.
type Catalog map[string]Cents

// Line builds an order line from the catalog; ok is false for an unknown SKU.
func (c Catalog) Line(sku string, qty int) (Line, bool) {
	p, ok := c[sku]
	if !ok {
		return Line{}, false
	}

	return Line{SKU: sku, Quantity: qty, Unit: p}, true
}
