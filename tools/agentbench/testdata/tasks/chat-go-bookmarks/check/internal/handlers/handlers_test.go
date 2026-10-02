package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"example.com/bookmarks/internal/handlers"
	"example.com/bookmarks/internal/model"
	"example.com/bookmarks/internal/store"
)

// newTestServer returns the API over a fresh store.
func newTestServer(t *testing.T) http.Handler {
	t.Helper()
	return handlers.New(store.New())
}

// do sends one request and returns the recorded response.
func do(t *testing.T, h http.Handler, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// decode unmarshals the response body into v.
func decode(t *testing.T, rec *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), v); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
}

// mustCreate posts a bookmark and fails the test unless it is created.
func mustCreate(t *testing.T, h http.Handler, body string) model.Bookmark {
	t.Helper()
	rec := do(t, h, "POST", "/bookmarks", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST %s: %d %s", body, rec.Code, rec.Body)
	}
	var b model.Bookmark
	decode(t, rec, &b)
	return b
}

// errorOf returns the "error" field of a JSON error response.
func errorOf(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type %q", ct)
	}
	var e struct {
		Error string `json:"error"`
	}
	decode(t, rec, &e)
	return e.Error
}

func TestCreateAndGet(t *testing.T) {
	h := newTestServer(t)
	b := mustCreate(t, h, `{"url":"https://go.dev","name":"Go","tags":["go","lang"]}`)
	if b.ID != 1 || b.Name != "Go" || len(b.Tags) != 2 {
		t.Fatalf("created %+v", b)
	}
	rec := do(t, h, "GET", "/bookmarks/1", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET: %d", rec.Code)
	}
	var got model.Bookmark
	decode(t, rec, &got)
	if got.URL != "https://go.dev" {
		t.Fatalf("got %+v", got)
	}
}

func TestCreateErrors(t *testing.T) {
	h := newTestServer(t)
	mustCreate(t, h, `{"url":"https://go.dev","name":"Go"}`)
	cases := []struct {
		body string
		code int
	}{
		{``, http.StatusBadRequest},
		{`{"url":"https://example.com"}`, http.StatusBadRequest},
		{`{"url":"https://example.com","name":"x","colour":"red"}`, http.StatusBadRequest},
		{`{"url":"https://go.dev/","name":"dup"}`, http.StatusConflict},
	}
	for _, c := range cases {
		rec := do(t, h, "POST", "/bookmarks", c.body)
		if rec.Code != c.code {
			t.Errorf("%s: %d, want %d", c.body, rec.Code, c.code)
		}
		if errorOf(t, rec) == "" {
			t.Errorf("%s: empty error", c.body)
		}
	}
}

func TestGetMissing(t *testing.T) {
	h := newTestServer(t)
	if rec := do(t, h, "GET", "/bookmarks/7", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("GET missing: %d", rec.Code)
	}
	if rec := do(t, h, "GET", "/bookmarks/abc", ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("GET abc: %d", rec.Code)
	}
}

func TestListSearch(t *testing.T) {
	h := newTestServer(t)
	mustCreate(t, h, `{"url":"https://go.dev","name":"Go"}`)
	mustCreate(t, h, `{"url":"https://www.rust-lang.org","name":"Rust"}`)
	var list []model.Bookmark
	decode(t, do(t, h, "GET", "/bookmarks?q=RUST", ""), &list)
	if len(list) != 1 || list[0].Name != "Rust" {
		t.Fatalf("q=RUST: %+v", list)
	}
	decode(t, do(t, h, "GET", "/bookmarks", ""), &list)
	if len(list) != 2 {
		t.Fatalf("all: %+v", list)
	}
}

func TestUpdate(t *testing.T) {
	h := newTestServer(t)
	mustCreate(t, h, `{"url":"https://go.dev","name":"Go"}`)
	rec := do(t, h, "PUT", "/bookmarks/1", `{"url":"https://go.dev","name":"Go home","tags":["go"]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT: %d %s", rec.Code, rec.Body)
	}
	var b model.Bookmark
	decode(t, rec, &b)
	if b.Name != "Go home" || len(b.Tags) != 1 {
		t.Fatalf("updated %+v", b)
	}
	if rec := do(t, h, "PUT", "/bookmarks/9", `{"url":"https://x.example","name":"x"}`); rec.Code != http.StatusNotFound {
		t.Fatalf("PUT missing: %d", rec.Code)
	}
}

func TestTags(t *testing.T) {
	h := newTestServer(t)
	mustCreate(t, h, `{"url":"https://go.dev","name":"Go","tags":["Go","lang"]}`)
	mustCreate(t, h, `{"url":"https://pkg.go.dev","name":"Packages","tags":["go"]}`)
	var counts []struct {
		Tag   string `json:"tag"`
		Count int    `json:"count"`
	}
	decode(t, do(t, h, "GET", "/tags", ""), &counts)
	if len(counts) != 2 || counts[0].Tag != "go" || counts[0].Count != 2 {
		t.Fatalf("tags: %+v", counts)
	}
}
