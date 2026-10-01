package ringbuf

import (
	"errors"
	"reflect"
	"testing"
)

func TestNew(t *testing.T) {
	if _, err := New[int](0); err == nil {
		t.Fatal("New(0) accepted")
	}
	b, err := New[int](3)
	if err != nil || b.Cap() != 3 || b.Len() != 0 {
		t.Fatalf("New(3) = %v, %v", b, err)
	}
}

func TestPushPopOverwrite(t *testing.T) {
	b, _ := New[int](3)
	if _, err := b.Pop(); !errors.Is(err, ErrEmpty) {
		t.Fatalf("Pop on empty: %v", err)
	}
	if _, err := b.Peek(); !errors.Is(err, ErrEmpty) {
		t.Fatalf("Peek on empty: %v", err)
	}
	for i := 1; i <= 5; i++ {
		b.Push(i)
	}
	if got := b.Slice(); !reflect.DeepEqual(got, []int{3, 4, 5}) || b.Dropped() != 2 {
		t.Fatalf("Slice = %v, dropped %d", got, b.Dropped())
	}
	if v, err := b.Peek(); err != nil || v != 3 {
		t.Fatalf("Peek = %v, %v", v, err)
	}
	if v, err := b.Pop(); err != nil || v != 3 || b.Len() != 2 {
		t.Fatalf("Pop = %v, %v, len %d", v, err, b.Len())
	}
	b.Push(6)
	if got := b.Slice(); !reflect.DeepEqual(got, []int{4, 5, 6}) {
		t.Fatalf("Slice = %v", got)
	}
}

func TestResetResize(t *testing.T) {
	b, _ := New[string](2)
	b.Push("a")
	b.Push("b")
	b.Push("c")
	if err := b.Resize(0); err == nil {
		t.Fatal("Resize(0) accepted")
	}
	if err := b.Resize(4); err != nil || !reflect.DeepEqual(b.Slice(), []string{"b", "c"}) || b.Cap() != 4 {
		t.Fatalf("Resize(4): %v %v", b.Slice(), err)
	}
	b.Push("d")
	if err := b.Resize(1); err != nil || !reflect.DeepEqual(b.Slice(), []string{"d"}) || b.Dropped() != 3 {
		t.Fatalf("Resize(1): %v dropped %d", b.Slice(), b.Dropped())
	}
	b.Reset()
	if b.Len() != 0 || b.Dropped() != 0 || len(b.Slice()) != 0 {
		t.Fatal("Reset left values")
	}
}
