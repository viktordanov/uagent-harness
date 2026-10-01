// Package shipping books parcels.
package shipping

import (
	"fmt"

	"example.com/shop/logx"
)

// Book returns a tracking number for a parcel.
func Book(orderID int, country string, grams int) (string, error) {
	logx.Logf("booking parcel for order %d to %s (%d g)", orderID, country, grams)
	if grams > 30000 {
		logx.Logf("parcel for order %d too heavy: %d g", orderID, grams)

		return "", fmt.Errorf("parcel too heavy: %d g", grams)
	}
	tn := fmt.Sprintf("%s-%06d", country, orderID)
	logx.Logf("booked %s", tn)

	return tn, nil
}

// Track logs a tracking event.
func Track(tn, event string) {
	logx.Logf("tracking %s: %s", tn, event)
}
