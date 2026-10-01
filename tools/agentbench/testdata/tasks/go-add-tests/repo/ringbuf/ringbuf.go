// Package ringbuf is a fixed-size FIFO buffer that overwrites its oldest
// element when full.
package ringbuf

import "errors"

// ErrEmpty is returned when reading from an empty buffer.
var ErrEmpty = errors.New("ringbuf: empty")

// Buffer holds up to Cap() values of type T.
type Buffer[T any] struct {
	data  []T
	head  int // index of the oldest element
	count int
	drops int
}

// New returns a buffer of capacity n; n must be at least 1.
func New[T any](n int) (*Buffer[T], error) {
	if n < 1 {
		return nil, errors.New("ringbuf: capacity must be at least 1")
	}

	return &Buffer[T]{data: make([]T, n)}, nil
}

// Cap is the buffer's capacity.
func (b *Buffer[T]) Cap() int { return len(b.data) }

// Len is the number of values held.
func (b *Buffer[T]) Len() int { return b.count }

// Dropped counts the values overwritten because the buffer was full.
func (b *Buffer[T]) Dropped() int { return b.drops }

// Push adds v, overwriting the oldest value when full.
func (b *Buffer[T]) Push(v T) {
	if b.count == len(b.data) {
		b.data[b.head] = v
		b.head = (b.head + 1) % len(b.data)
		b.drops++

		return
	}
	b.data[(b.head+b.count)%len(b.data)] = v
	b.count++
}

// Pop removes and returns the oldest value.
func (b *Buffer[T]) Pop() (T, error) {
	var zero T
	if b.count == 0 {
		return zero, ErrEmpty
	}
	v := b.data[b.head]
	b.data[b.head] = zero
	b.head = (b.head + 1) % len(b.data)
	b.count--

	return v, nil
}

// Peek returns the oldest value without removing it.
func (b *Buffer[T]) Peek() (T, error) {
	var zero T
	if b.count == 0 {
		return zero, ErrEmpty
	}

	return b.data[b.head], nil
}

// Slice returns the values from oldest to newest.
func (b *Buffer[T]) Slice() []T {
	out := make([]T, 0, b.count)
	for i := range b.count {
		out = append(out, b.data[(b.head+i)%len(b.data)])
	}

	return out
}

// Reset empties the buffer and clears the drop count.
func (b *Buffer[T]) Reset() {
	clear(b.data)
	b.head, b.count, b.drops = 0, 0, 0
}

// Resize changes the capacity to n, keeping the newest values that fit.
func (b *Buffer[T]) Resize(n int) error {
	if n < 1 {
		return errors.New("ringbuf: capacity must be at least 1")
	}
	vals := b.Slice()
	if len(vals) > n {
		b.drops += len(vals) - n
		vals = vals[len(vals)-n:]
	}
	b.data = make([]T, n)
	copy(b.data, vals)
	b.head, b.count = 0, len(vals)

	return nil
}
