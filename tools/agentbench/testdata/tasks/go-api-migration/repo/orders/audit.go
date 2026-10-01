package orders

import "example.com/shop/logx"

// Audit logs a summary of orders.
func Audit(os []Order) int {
	total := 0
	for _, o := range os {
		if o.Cents > 100000 {
			logx.Logf("large order %d by %s: %d cents", o.ID, o.User, o.Cents)
		}
		total += o.Cents
	}
	logx.Logf("audited %d orders, %d cents in total", len(os), total)

	return total
}
