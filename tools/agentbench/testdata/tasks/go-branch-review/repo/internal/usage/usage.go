// Package usage records billable usage per tenant.
package usage

import (
	"context"
	"errors"
	"sync"
)

// ErrClosed is returned after Close.
var ErrClosed = errors.New("usage: recorder closed")

// Recorder records units a tenant used.
type Recorder interface {
	Record(ctx context.Context, tenant string, units int) error
}

// Memory is an in-memory Recorder.
type Memory struct {
	mu     sync.Mutex
	units  map[string]int
	closed bool
}

// NewMemory returns an empty recorder.
func NewMemory() *Memory { return &Memory{units: map[string]int{}} }

// Record adds units to the tenant's total.
func (m *Memory) Record(_ context.Context, tenant string, units int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return ErrClosed
	}
	m.units[tenant] += units

	return nil
}

// Total returns a tenant's units.
func (m *Memory) Total(tenant string) int {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.units[tenant]
}

// Close stops recording.
func (m *Memory) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
}
