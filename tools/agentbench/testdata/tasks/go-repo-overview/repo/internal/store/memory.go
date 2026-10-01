package store

import (
	"context"
	"sync"
	"time"
)

// MemStore is an in-memory Store. Reads return copies, so a caller can
// never change what another caller sees.
type MemStore struct {
	mu        sync.RWMutex
	seq       int64
	shipments map[string]*Shipment
	subs      map[string][]Subscriber
	now       func() time.Time
}

// NewMemStore returns an empty store.
func NewMemStore() *MemStore {
	return &MemStore{shipments: map[string]*Shipment{}, subs: map[string][]Subscriber{}, now: time.Now}
}

// AppendEvent numbers the event, records it, and moves the shipment to its
// status. An event for an unknown shipment creates the shipment.
func (m *MemStore) AppendEvent(_ context.Context, ev Event) (Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seq++
	ev.Seq, ev.At = m.seq, m.now().UTC()
	sh, ok := m.shipments[ev.ShipmentID]
	if !ok {
		sh = &Shipment{ID: ev.ShipmentID}
		m.shipments[ev.ShipmentID] = sh
	}
	sh.Events = append(sh.Events, ev)
	sh.Status = ev.Status

	return ev, nil
}

// Shipment returns a copy of the shipment.
func (m *MemStore) Shipment(_ context.Context, id string) (Shipment, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	sh, ok := m.shipments[id]
	if !ok {
		return Shipment{}, ErrNotFound
	}
	cp := *sh
	cp.Events = append([]Event(nil), sh.Events...)

	return cp, nil
}

// AddSubscriber adds a webhook for a shipment.
func (m *MemStore) AddSubscriber(_ context.Context, sub Subscriber) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.subs[sub.ShipmentID] = append(m.subs[sub.ShipmentID], sub)

	return nil
}

// Subscribers returns a copy of a shipment's webhooks.
func (m *MemStore) Subscribers(_ context.Context, id string) ([]Subscriber, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return append([]Subscriber(nil), m.subs[id]...), nil
}
