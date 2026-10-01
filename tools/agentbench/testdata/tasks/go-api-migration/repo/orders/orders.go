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
	logx.Logf("placing order for %s with %d items", user, items)
	if items == 0 {
		logx.Logf("rejected empty order from %s", user)

		return Order{}, ErrEmpty
	}
	o := Order{ID: nextID, User: user, Items: items, Cents: cents}
	nextID++
	logx.Logf("placed order %d for %s, total %d cents", o.ID, user, cents)

	return o, nil
}

// Cancel cancels an order.
func Cancel(o Order, reason string) {
	logx.Logf("cancelled order %d: %s", o.ID, reason)
}
