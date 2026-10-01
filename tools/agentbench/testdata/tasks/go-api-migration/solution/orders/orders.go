// Package orders places and cancels orders.
package orders

import (
	"errors"

	"example.com/shop/logx"
)

// Order is a placed order.
type Order struct {
	ID    int
	User  string
	Items int
	Cents int
}

var nextID = 1

// ErrEmpty is returned for an order with no items.
var ErrEmpty = errors.New("empty order")

// Place creates an order.
func Place(user string, items, cents int) (Order, error) {
	logx.Info("placing order", "user", user, "items", items)
	if items == 0 {
		logx.Error("rejected empty order", "user", user)

		return Order{}, ErrEmpty
	}
	o := Order{ID: nextID, User: user, Items: items, Cents: cents}
	nextID++
	logx.Info("placed order", "id", o.ID, "user", user, "cents", cents)

	return o, nil
}

// Cancel cancels an order.
func Cancel(o Order, reason string) {
	logx.Info("cancelled order", "id", o.ID, "reason", reason)
}
