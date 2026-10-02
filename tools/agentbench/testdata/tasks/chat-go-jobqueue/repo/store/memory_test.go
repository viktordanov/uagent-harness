package store

import (
	"errors"
	"testing"
	"time"

	"example.com/jobq/internal/clock"
	"example.com/jobq/queue"
)

func mustAdd(t *testing.T, s queue.Store, kind, payload string) queue.Task {
	t.Helper()
	task, err := s.Add(queue.NewTask(kind, payload))
	if err != nil {
		t.Fatalf("Add(%s, %q): %v", kind, payload, err)
	}
	return task
}

func TestMemoryAddAssignsIDs(t *testing.T) {
	m := NewMemory()
	a := mustAdd(t, m, "echo", "a")
	b := mustAdd(t, m, "echo", "b")
	if a.ID != "job-0001" || b.ID != "job-0002" {
		t.Fatalf("IDs %q and %q", a.ID, b.ID)
	}
	if a.State != queue.Pending {
		t.Fatalf("new job is %s", a.State)
	}
	got, err := m.Get(b.ID)
	if err != nil || got.Payload != "b" {
		t.Fatalf("Get(%s) = %+v, %v", b.ID, got, err)
	}
	if m.Len() != 2 {
		t.Fatalf("Len() = %d", m.Len())
	}
}

func TestMemoryRejectsInvalid(t *testing.T) {
	m := NewMemory()
	if _, err := m.Add(queue.NewTask("", "x")); err == nil {
		t.Fatal("Add accepted an empty kind")
	}
	if _, err := m.Add(queue.NewTask("two words", "x")); err == nil {
		t.Fatal("Add accepted a kind with a space")
	}
}

func TestMemoryNotFound(t *testing.T) {
	m := NewMemory()
	if _, err := m.Get("job-0042"); !errors.Is(err, queue.ErrNotFound) {
		t.Fatalf("Get: %v", err)
	}
	if err := m.Update(queue.Task{ID: "job-0042", Kind: "echo", State: queue.Done}); !errors.Is(err, queue.ErrNotFound) {
		t.Fatalf("Update: %v", err)
	}
	if err := m.Delete("job-0042"); !errors.Is(err, queue.ErrNotFound) {
		t.Fatalf("Delete: %v", err)
	}
}

func TestMemoryClaimOldestReady(t *testing.T) {
	m := NewMemory()
	now := clock.Epoch
	a := mustAdd(t, m, "echo", "a")
	b := mustAdd(t, m, "echo", "b")

	// Push a into the future: b is the oldest ready job.
	a.RunAt = now.Add(time.Minute)
	if err := m.Update(a); err != nil {
		t.Fatal(err)
	}
	got, ok, err := m.Claim(now)
	if err != nil || !ok || got.ID != b.ID {
		t.Fatalf("Claim = %v, %v, %v; want %s", got, ok, err, b.ID)
	}
	if got.State != queue.Running || got.Attempts != 1 {
		t.Fatalf("claimed job: %+v", got)
	}
	if _, ok, _ := m.Claim(now); ok {
		t.Fatal("claimed a job that is not ready")
	}
	got, ok, _ = m.Claim(now.Add(time.Minute))
	if !ok || got.ID != a.ID {
		t.Fatalf("Claim after RunAt = %v, %v", got, ok)
	}
}

func TestMemoryListAndCounts(t *testing.T) {
	m := NewMemory()
	for _, p := range []string{"a", "b", "c"} {
		mustAdd(t, m, "echo", p)
	}
	if _, _, err := m.Claim(clock.Epoch); err != nil {
		t.Fatal(err)
	}
	pending, err := m.List(queue.Pending)
	if err != nil || len(pending) != 2 || pending[0].Payload != "b" {
		t.Fatalf("List(pending) = %v, %v", pending, err)
	}
	all, _ := m.List("")
	if len(all) != 3 {
		t.Fatalf("List(\"\") has %d jobs", len(all))
	}
	if _, err := m.List(queue.State("bogus")); err == nil {
		t.Fatal("List accepted an unknown state")
	}
	counts, _ := m.Counts()
	if counts[queue.Pending] != 2 || counts[queue.Running] != 1 || counts[queue.Done] != 0 {
		t.Fatalf("Counts() = %v", counts)
	}
	for _, st := range queue.States() {
		if _, ok := counts[st]; !ok {
			t.Errorf("Counts() has no %s", st)
		}
	}
}

func TestMemoryDeleteKeepsOrder(t *testing.T) {
	m := NewMemory()
	a := mustAdd(t, m, "echo", "a")
	mustAdd(t, m, "echo", "b")
	mustAdd(t, m, "echo", "c")
	if err := m.Delete(a.ID); err != nil {
		t.Fatal(err)
	}
	all, _ := m.List("")
	if len(all) != 2 || all[0].Payload != "b" || all[1].Payload != "c" {
		t.Fatalf("after Delete: %v", all)
	}
	d := mustAdd(t, m, "echo", "d")
	if d.ID != "job-0004" {
		t.Fatalf("an ID was reused: %s", d.ID)
	}
}
