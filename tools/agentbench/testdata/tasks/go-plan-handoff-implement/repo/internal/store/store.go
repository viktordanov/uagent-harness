// Package store keeps the short links.
package store

import (
	"crypto/rand"
	"errors"
	"sync"
)

// ErrNotFound is returned for a code with no link.
var ErrNotFound = errors.New("store: no such code")

// Store maps codes to URLs.
type Store interface {
	// Put saves url under a new code and returns the code.
	Put(url string) (string, error)
	// Get returns the URL of code, or ErrNotFound.
	Get(code string) (string, error)
}

// Memory is a Store in memory.
type Memory struct {
	mu    sync.RWMutex
	links map[string]string
}

// NewMemory returns an empty Memory.
func NewMemory() *Memory { return &Memory{links: map[string]string{}} }

// Put saves url under a new random code.
func (m *Memory) Put(url string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for {
		code, err := newCode()
		if err != nil {
			return "", err
		}
		if _, taken := m.links[code]; !taken {
			m.links[code] = url

			return code, nil
		}
	}
}

// Get returns the URL saved under code.
func (m *Memory) Get(code string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	url, ok := m.links[code]
	if !ok {
		return "", ErrNotFound
	}

	return url, nil
}

const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"

func newCode() (string, error) {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}

	return string(b), nil
}
