// Package shipping books parcels.
package shipping

import (
	"fmt"

	"example.com/shop/logx"
)

// Book returns a tracking number for a parcel.
func Book(orderID int, country string, grams int) (string, error) {
	logx.Info("booking parcel", "order", orderID, "country", country, "grams", grams)
	if grams > 30000 {
		logx.Error("parcel too heavy", "order", orderID, "grams", grams)

		return "", fmt.Errorf("parcel too heavy: %d g", grams)
	}
	tn := fmt.Sprintf("%s-%06d", country, orderID)
	logx.Info("booked parcel", "tracking", tn)

	return tn, nil
}

// Track logs a tracking event.
func Track(tn, event string) {
	logx.Info("tracking event", "tracking", tn, "event", event)
}
