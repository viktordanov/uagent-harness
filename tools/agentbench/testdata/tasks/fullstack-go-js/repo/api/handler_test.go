package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func clock() func() time.Time {
	t := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	return func() time.Time { t = t.Add(time.Minute); return t }
}

func TestListNewestFirst(t *testing.T) {
	s := NewStore(clock())
	s.Add("a")
	s.Add("b")
	rec := httptest.NewRecorder()
	Handler{Store: s}.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/tasks", nil))
	var got []Task
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Title != "b" {
		t.Fatalf("got %+v", got)
	}
}

func TestCreateNeedsTitle(t *testing.T) {
	rec := httptest.NewRecorder()
	Handler{Store: NewStore(clock())}.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/tasks", strings.NewReader(`{"title":" "}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code %d", rec.Code)
	}
}
