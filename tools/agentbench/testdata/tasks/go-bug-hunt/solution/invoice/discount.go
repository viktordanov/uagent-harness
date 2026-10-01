package invoice

import "math"

// applyDiscount returns the subtotal after the code's percentage is taken
// off, rounded half up to the cent like every other percentage.
func applyDiscount(subtotal Cents, code Code) Cents {
	if code.Percent <= 0 {
		return subtotal
	}
	bp := int64(math.Round((100 - code.Percent) * 100))

	return mulRate(subtotal, bp)
}
