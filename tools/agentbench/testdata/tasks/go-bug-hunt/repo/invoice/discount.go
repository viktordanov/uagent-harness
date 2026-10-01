package invoice

// applyDiscount returns the subtotal after the code's percentage is taken
// off, rounded to the cent.
func applyDiscount(subtotal Cents, code Code) Cents {
	if code.Percent <= 0 {
		return subtotal
	}
	factor := (100 - code.Percent) / 100

	return Cents(float64(subtotal) * factor)
}
