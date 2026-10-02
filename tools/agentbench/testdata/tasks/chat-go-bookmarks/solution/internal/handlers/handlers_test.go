package handlers_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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

func TestCreateAcceptsOldTitle(t *testing.T) {
	h := newTestServer(t)
	rec := do(t, h, "POST", "/bookmarks", `{"url":"https://go.dev","title":"Go"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST: %d %s", rec.Code, rec.Body)
	}
	var raw map[string]any
	decode(t, rec, &raw)
	if raw["name"] != "Go" {
		t.Fatalf("name %v", raw["name"])
	}
	if _, ok := raw["title"]; ok {
		t.Fatal("response still has a title key")
	}
}

func TestListTag(t *testing.T) {
	h := newTestServer(t)
	mustCreate(t, h, `{"url":"https://go.dev","name":"Go","tags":["Go","lang"]}`)
	mustCreate(t, h, `{"url":"https://www.rust-lang.org","name":"Rust","tags":["lang"]}`)
	mustCreate(t, h, `{"url":"https://example.com","name":"Example"}`)
	var list []model.Bookmark
	decode(t, do(t, h, "GET", "/bookmarks?tag=go", ""), &list)
	if len(list) != 1 || list[0].Name != "Go" {
		t.Fatalf("tag=go: %+v", list)
	}
	decode(t, do(t, h, "GET", "/bookmarks?tag=LANG", ""), &list)
	if len(list) != 2 {
		t.Fatalf("tag=LANG: %+v", list)
	}
	decode(t, do(t, h, "GET", "/bookmarks?tag=none", ""), &list)
	if len(list) != 0 {
		t.Fatalf("tag=none: %+v", list)
	}
}

func TestListPaging(t *testing.T) {
	h := newTestServer(t)
	for i := range 5 {
		mustCreate(t, h, fmt.Sprintf(`{"url":"https://e%d.example","name":"e%d","tags":["t"]}`, i, i))
	}
	cases := []struct {
		query string
		ids   []int64
	}{
		{"limit=2", []int64{1, 2}},
		{"limit=2&offset=1", []int64{2, 3}},
		{"offset=3", []int64{4, 5}},
		{"offset=9", nil},
		{"limit=0", nil},
		{"tag=T&limit=1&offset=4", []int64{5}},
	}
	for _, c := range cases {
		var list []model.Bookmark
		decode(t, do(t, h, "GET", "/bookmarks?"+c.query, ""), &list)
		var ids []int64
		for _, b := range list {
			ids = append(ids, b.ID)
		}
		if fmt.Sprint(ids) != fmt.Sprint(c.ids) {
			t.Errorf("%s: ids %v, want %v", c.query, ids, c.ids)
		}
	}
	for _, q := range []string{"limit=-1", "limit=x", "offset=-2", "offset=1.5"} {
		rec := do(t, h, "GET", "/bookmarks?"+q, "")
		if rec.Code != http.StatusBadRequest || errorOf(t, rec) == "" {
			t.Errorf("%s: %d %s", q, rec.Code, rec.Body)
		}
	}
}

func TestDelete(t *testing.T) {
	h := newTestServer(t)
	mustCreate(t, h, `{"url":"https://go.dev","name":"Go"}`)
	if rec := do(t, h, "DELETE", "/bookmarks/1", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE: %d", rec.Code)
	}
	if rec := do(t, h, "GET", "/bookmarks/1", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("GET after DELETE: %d", rec.Code)
	}
	rec := do(t, h, "DELETE", "/bookmarks/1", "")
	if rec.Code != http.StatusNotFound || errorOf(t, rec) == "" {
		t.Fatalf("DELETE missing: %d", rec.Code)
	}
}

func TestConcurrentRequests(t *testing.T) {
	h := newTestServer(t)
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range 20 {
				body := fmt.Sprintf(`{"url":"https://e%d-%d.example","name":"x"}`, i, j)
				if rec := do(t, h, "POST", "/bookmarks", body); rec.Code != http.StatusCreated {
					t.Errorf("POST: %d", rec.Code)
				}
				do(t, h, "GET", "/bookmarks?limit=5", "")
				do(t, h, "GET", "/tags", "")
			}
		}()
	}
	wg.Wait()
	var list []model.Bookmark
	decode(t, do(t, h, "GET", "/bookmarks", ""), &list)
	if len(list) != 160 {
		t.Fatalf("%d bookmarks, want 160", len(list))
	}
}
