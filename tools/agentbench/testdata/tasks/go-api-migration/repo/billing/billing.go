// Package billing charges cards.
package billing

import (
	"errors"

	"example.com/shop/logx"
)

// ErrDeclined is a declined charge.
var ErrDeclined = errors.New("card declined")

// Charge charges cents to a card; cards ending in 0000 decline.
func Charge(card string, cents int) error {
	logx.Logf("charging %d cents to card ending %s", cents, last4(card))
	if last4(card) == "0000" {
		logx.Logf("card ending %s declined for %d cents", last4(card), cents)

		return ErrDeclined
	}
	logx.Logf("charged %d cents", cents)

	return nil
}

// Refund refunds cents to a card.
func Refund(card string, cents int) {
	logx.Logf("refunded %d cents to card ending %s", cents, last4(card))
}

func last4(card string) string {
	if len(card) < 4 {
		return card
	}

	return card[len(card)-4:]
}
