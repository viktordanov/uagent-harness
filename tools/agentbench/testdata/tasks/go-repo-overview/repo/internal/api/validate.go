package api

import "errors"

var knownStatuses = map[string]bool{
	"created": true, "picked_up": true, "in_transit": true, "out_for_delivery": true, "delivered": true, "exception": true,
}

func validate(req eventRequest) error {
	switch {
	case req.ShipmentID == "":
		return errors.New("shipment_id is required")
	case !knownStatuses[req.Status]:
		return errors.New("unknown status")
	}

	return nil
}
