package apicheck_test

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

// The agentbench check.

var _ = model.Bookmark{Name: "renamed"}

func hiddenDo(h http.Handler, method, target, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func hiddenCreate(t *testing.T, h http.Handler, body string) map[string]any {
	t.Helper()
	rec := hiddenDo(h, "POST", "/bookmarks", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST %s: %d %s", body, rec.Code, rec.Body)
	}
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("POST %s: body %q: %v", body, rec.Body, err)
	}
	return m
}

func hiddenList(t *testing.T, h http.Handler, query string) []int {
	t.Helper()
	rec := hiddenDo(h, "GET", "/bookmarks"+query, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /bookmarks%s: %d %s", query, rec.Code, rec.Body)
	}
	var list []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("GET /bookmarks%s: not a JSON array: %q", query, rec.Body)
	}
	ids := []int{}
	for _, b := range list {
		id, _ := b["id"].(float64)
		ids = append(ids, int(id))
	}
	return ids
}

func hiddenIsJSONError(rec *httptest.ResponseRecorder) bool {
	if !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
		return false
	}
	var e map[string]any
	if json.Unmarshal(rec.Body.Bytes(), &e) != nil {
		return false
	}
	s, _ := e["error"].(string)
	return s != ""
}

func TestHiddenTagFilter(t *testing.T) {
	h := handlers.New(store.New())
	hiddenCreate(t, h, `{"url":"https://go.dev","name":"Go","tags":["Go","lang"]}`)
	hiddenCreate(t, h, `{"url":"https://www.rust-lang.org","name":"Rust","tags":["lang","systems"]}`)
	hiddenCreate(t, h, `{"url":"https://example.com","name":"Example"}`)
	cases := map[string]string{
		"?tag=go":          "[1]",
		"?tag=GO":          "[1]",
		"?tag=LANG":        "[1 2]",
		"?tag=Systems":     "[2]",
		"?tag=nope":        "[]",
		"?tag=lang&q=rust": "[2]",
		"":                 "[1 2 3]",
	}
	for q, want := range cases {
		if got := fmt.Sprint(hiddenList(t, h, q)); got != want {
			t.Errorf("GET /bookmarks%s: ids %s, want %s", q, got, want)
		}
	}
}

func TestHiddenPaging(t *testing.T) {
	h := handlers.New(store.New())
	for i := 1; i <= 6; i++ {
		tags := `["odd"]`
		if i%2 == 0 {
			tags = `["even"]`
		}
		hiddenCreate(t, h, fmt.Sprintf(`{"url":"https://e%d.example","name":"e%d","tags":%s}`, i, i, tags))
	}
	cases := map[string]string{
		"?limit=2":                   "[1 2]",
		"?limit=2&offset=1":          "[2 3]",
		"?offset=4":                  "[5 6]",
		"?offset=10":                 "[]",
		"?limit=100":                 "[1 2 3 4 5 6]",
		"?tag=EVEN&limit=2&offset=1": "[4 6]",
		"?tag=odd&offset=2":          "[5]",
	}
	for q, want := range cases {
		if got := fmt.Sprint(hiddenList(t, h, q)); got != want {
			t.Errorf("GET /bookmarks%s: ids %s, want %s", q, got, want)
		}
	}
	for _, q := range []string{"limit=-1", "limit=abc", "offset=-3", "offset=x", "limit=2&offset=-1"} {
		rec := hiddenDo(h, "GET", "/bookmarks?"+q, "")
		if rec.Code != http.StatusBadRequest || !hiddenIsJSONError(rec) {
			t.Errorf("GET /bookmarks?%s: %d %q, want 400 with a JSON error body", q, rec.Code, rec.Body)
		}
	}
}

func TestHiddenNameRename(t *testing.T) {
	h := handlers.New(store.New())
	b := hiddenCreate(t, h, `{"url":"https://go.dev","name":"Go"}`)
	if b["name"] != "Go" {
		t.Errorf("POST name: response name = %v", b["name"])
	}
	if _, ok := b["title"]; ok {
		t.Errorf("response still has a title key: %v", b)
	}
	old := hiddenCreate(t, h, `{"url":"https://pkg.go.dev","title":"Packages"}`)
	if old["name"] != "Packages" {
		t.Errorf("POST title: response name = %v, want Packages", old["name"])
	}
	rec := hiddenDo(h, "PUT", "/bookmarks/1", `{"url":"https://go.dev","title":"Go home"}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"name":"Go home"`) {
		t.Errorf("PUT with title: %d %s", rec.Code, rec.Body)
	}
	rec = hiddenDo(h, "PUT", "/bookmarks/1", `{"url":"https://go.dev","name":"Go site"}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"name":"Go site"`) {
		t.Errorf("PUT with name: %d %s", rec.Code, rec.Body)
	}
	if rec := hiddenDo(h, "POST", "/bookmarks", `{"url":"https://x.example"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("POST without name: %d", rec.Code)
	}
	if rec := hiddenDo(h, "GET", "/bookmarks/2", ""); !strings.Contains(rec.Body.String(), `"name":"Packages"`) {
		t.Errorf("GET: %s", rec.Body)
	}
}

func TestHiddenDelete(t *testing.T) {
	h := handlers.New(store.New())
	hiddenCreate(t, h, `{"url":"https://go.dev","name":"Go"}`)
	hiddenCreate(t, h, `{"url":"https://pkg.go.dev","name":"Packages"}`)
	rec := hiddenDo(h, "DELETE", "/bookmarks/1", "")
	if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
		t.Fatalf("DELETE: %d %q, want 204 and no body", rec.Code, rec.Body)
	}
	if rec := hiddenDo(h, "GET", "/bookmarks/1", ""); rec.Code != http.StatusNotFound {
		t.Errorf("GET after DELETE: %d", rec.Code)
	}
	for _, target := range []string{"/bookmarks/1", "/bookmarks/42"} {
		rec := hiddenDo(h, "DELETE", target, "")
		if rec.Code != http.StatusNotFound || !hiddenIsJSONError(rec) {
			t.Errorf("DELETE %s: %d %q, want 404 with a JSON error body", target, rec.Code, rec.Body)
		}
	}
	if got := fmt.Sprint(hiddenList(t, h, "")); got != "[2]" {
		t.Errorf("after DELETE: ids %s", got)
	}
}

func TestHiddenConcurrent(t *testing.T) {
	h := handlers.New(store.New())
	const workers, each = 8, 30
	var wg sync.WaitGroup
	for w := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range each {
				body := fmt.Sprintf(`{"url":"https://w%d-%d.example","name":"n","tags":["t%d"]}`, w, j, w)
				rec := hiddenDo(h, "POST", "/bookmarks", body)
				if rec.Code != http.StatusCreated {
					t.Errorf("POST: %d %s", rec.Code, rec.Body)
					return
				}
				var b map[string]any
				json.Unmarshal(rec.Body.Bytes(), &b)
				id := int(b["id"].(float64))
				hiddenDo(h, "GET", fmt.Sprintf("/bookmarks/%d", id), "")
				hiddenDo(h, "GET", "/bookmarks?limit=3&tag=t1", "")
				hiddenDo(h, "GET", "/tags", "")
				hiddenDo(h, "PUT", fmt.Sprintf("/bookmarks/%d", id), fmt.Sprintf(`{"url":"https://w%d-%d.example","name":"m"}`, w, j))
				if j%3 == 0 {
					if rec := hiddenDo(h, "DELETE", fmt.Sprintf("/bookmarks/%d", id), ""); rec.Code != http.StatusNoContent {
						t.Errorf("DELETE: %d", rec.Code)
					}
				}
			}
		}()
	}
	wg.Wait()
	ids := hiddenList(t, h, "")
	if len(ids) != workers*each*2/3 {
		t.Fatalf("%d bookmarks left, want %d", len(ids), workers*each*2/3)
	}
	seen := map[int]bool{}
	for _, id := range ids {
		if seen[id] {
			t.Fatalf("duplicate id %d", id)
		}
		seen[id] = true
	}
}
