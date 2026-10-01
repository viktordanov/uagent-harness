package orders

import "example.com/shop/logx"

// Audit logs a summary of orders.
func Audit(os []Order) int {
	total := 0
	for _, o := range os {
		if o.Cents > 100000 {
			logx.Info("large order", "id", o.ID, "user", o.User, "cents", o.Cents)
		}
		total += o.Cents
	}
	logx.Info("audited orders", "count", len(os), "cents", total)

	return total
}
