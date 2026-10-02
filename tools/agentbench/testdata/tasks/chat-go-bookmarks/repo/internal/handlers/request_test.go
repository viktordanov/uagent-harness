package handlers

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeBookmarkRequest(t *testing.T) {
	r := httptest.NewRequest("POST", "/bookmarks", strings.NewReader(`{"url":"https://go.dev","title":"Go","tags":["a"]}`))
	req, err := decodeBookmarkRequest(httptest.NewRecorder(), r)
	if err != nil {
		t.Fatal(err)
	}
	b := req.bookmark()
	if b.URL != "https://go.dev" || b.Title != "Go" || len(b.Tags) != 1 {
		t.Fatalf("got %+v", b)
	}
}

func TestDecodeBookmarkRequestErrors(t *testing.T) {
	for _, body := range []string{``, `{`, `{"url":1}`, `{"titel":"typo"}`, `{} {}`} {
		r := httptest.NewRequest("POST", "/bookmarks", strings.NewReader(body))
		if _, err := decodeBookmarkRequest(httptest.NewRecorder(), r); err == nil {
			t.Errorf("%q: no error", body)
		}
	}
}
