package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func list(t *testing.T, s *Store, query string) (int, []Task) {
	t.Helper()
	rec := httptest.NewRecorder()
	Handler{Store: s}.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/tasks"+query, nil))
	if rec.Code != http.StatusOK {
		return rec.Code, nil
	}
	var got []Task
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}

	return rec.Code, got
}

func TestHiddenStatusFilter(t *testing.T) {
	s := NewStore(clock())
	a := s.Add("a")
	b := s.Add("b")
	s.Add("c")
	s.SetStatus(a.ID, StatusDone)
	s.SetStatus(b.ID, StatusDoing)

	if _, all := list(t, s, ""); len(all) != 3 {
		t.Fatalf("no filter: %d tasks", len(all))
	}
	for status, want := range map[string]string{"done": "a", "doing": "b", "todo": "c"} {
		code, got := list(t, s, "?status="+status)
		if code != http.StatusOK || len(got) != 1 || got[0].Title != want {
			t.Errorf("status=%s: code %d, %+v", status, code, got)
		}
	}
	s.Add("d")
	if _, todo := list(t, s, "?status=todo"); len(todo) != 2 || todo[0].Title != "d" {
		t.Errorf("filtered list not newest first: %+v", todo)
	}
}

func TestHiddenBadStatus(t *testing.T) {
	rec := httptest.NewRecorder()
	Handler{Store: NewStore(clock())}.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/tasks?status=archived", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code %d", rec.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body["error"] == "" {
		t.Fatalf("want a JSON error, got %q", rec.Body.String())
	}
}
