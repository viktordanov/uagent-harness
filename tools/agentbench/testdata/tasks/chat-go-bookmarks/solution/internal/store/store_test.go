package store

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"example.com/bookmarks/internal/model"
)

func fixedClock() func() time.Time {
	t := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	return func() time.Time { return t }
}

func TestCreateAssignsIDs(t *testing.T) {
	s := New()
	s.SetClock(fixedClock())
	a, err := s.Create(model.Bookmark{URL: "https://go.dev", Name: "Go"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.Create(model.Bookmark{URL: "https://pkg.go.dev", Name: "Packages"})
	if err != nil {
		t.Fatal(err)
	}
	if a.ID != 1 || b.ID != 2 {
		t.Fatalf("ids %d, %d; want 1, 2", a.ID, b.ID)
	}
	if !a.CreatedAt.Equal(fixedClock()()) {
		t.Fatalf("CreatedAt %v", a.CreatedAt)
	}
}

func TestCreateRejectsDuplicateURL(t *testing.T) {
	s := New()
	if _, err := s.Create(model.Bookmark{URL: "https://go.dev/", Name: "Go"}); err != nil {
		t.Fatal(err)
	}
	_, err := s.Create(model.Bookmark{URL: "https://GO.dev", Name: "Go again"})
	if !errors.Is(err, ErrDuplicateURL) {
		t.Fatalf("got %v, want ErrDuplicateURL", err)
	}
}

func TestGetAndUpdate(t *testing.T) {
	s := New()
	b, _ := s.Create(model.Bookmark{URL: "https://go.dev", Name: "Go", Tags: []string{"go"}})
	if _, err := s.Get(99); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get(99): %v", err)
	}
	u, err := s.Update(b.ID, model.Bookmark{URL: "https://go.dev", Name: "The Go site"})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(b.ID)
	if got.Name != "The Go site" || len(got.Tags) != 0 || !got.CreatedAt.Equal(u.CreatedAt) {
		t.Fatalf("after update: %+v", got)
	}
}

func TestListIsOrderedAndCopied(t *testing.T) {
	s := New()
	for _, u := range []string{"https://a.example", "https://b.example", "https://c.example"} {
		if _, err := s.Create(model.Bookmark{URL: u, Name: u, Tags: []string{"x"}}); err != nil {
			t.Fatal(err)
		}
	}
	list := s.List()
	if len(list) != 3 || list[0].ID != 1 || list[2].ID != 3 {
		t.Fatalf("List: %+v", list)
	}
	list[0].Tags[0] = "changed"
	if b, _ := s.Get(1); b.Tags[0] != "x" {
		t.Fatal("List shares memory with the store")
	}
}

func TestDelete(t *testing.T) {
	s := New()
	b, _ := s.Create(model.Bookmark{URL: "https://go.dev", Name: "Go"})
	if err := s.Delete(b.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(b.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get after Delete: %v", err)
	}
	if err := s.Delete(b.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second Delete: %v", err)
	}
	c, _ := s.Create(model.Bookmark{URL: "https://go.dev", Name: "Go"})
	if c.ID == b.ID {
		t.Fatal("id reused after Delete")
	}
}

func TestConcurrentUse(t *testing.T) {
	s := New()
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range 25 {
				b, err := s.Create(model.Bookmark{URL: fmt.Sprintf("https://e%d-%d.example", i, j), Name: "x"})
				if err != nil {
					t.Error(err)
					return
				}
				s.List()
				s.Get(b.ID)
				if j%5 == 0 {
					s.Delete(b.ID)
				}
			}
		}()
	}
	wg.Wait()
	if s.Len() != 8*20 {
		t.Fatalf("Len %d, want %d", s.Len(), 8*20)
	}
}
