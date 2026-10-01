// Package store keeps shipments, their events, and their subscribers.
package store

import (
	"context"
	"errors"
	"time"
)

// ErrNotFound is returned for an unknown shipment.
var ErrNotFound = errors.New("not found")

// Event is one status change of a shipment.
type Event struct {
	Seq        int64     `json:"seq"`
	ShipmentID string    `json:"shipment_id"`
	Status     string    `json:"status"`
	Location   string    `json:"location"`
	At         time.Time `json:"at"`
}

// Shipment is a shipment's current state and history.
type Shipment struct {
	ID     string  `json:"id"`
	Status string  `json:"status"`
	Events []Event `json:"events"`
}

// Subscriber is a webhook to call on a shipment's events.
type Subscriber struct {
	ShipmentID string `json:"shipment_id"`
	URL        string `json:"url"`
	Secret     string `json:"secret"`
}

// Store is what the API needs from storage.
type Store interface {
	AppendEvent(ctx context.Context, ev Event) (Event, error)
	Shipment(ctx context.Context, id string) (Shipment, error)
	AddSubscriber(ctx context.Context, sub Subscriber) error
	Subscribers(ctx context.Context, shipmentID string) ([]Subscriber, error)
}
