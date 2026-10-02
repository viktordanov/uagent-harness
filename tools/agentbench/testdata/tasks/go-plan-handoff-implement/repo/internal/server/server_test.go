package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"example.com/shorty/internal/store"
)

func TestShortenAndRedirect(t *testing.T) {
	srv := httptest.NewServer(New(store.NewMemory()))
	defer srv.Close()
	res, err := http.Post(srv.URL+"/shorten", "application/json", strings.NewReader(`{"url":"https://example.com/x"}`))
	if err != nil {
		t.Fatal(err)
	}
	var body struct{ Code string }
	_ = json.NewDecoder(res.Body).Decode(&body)
	res.Body.Close()
	if res.StatusCode != http.StatusCreated || body.Code == "" {
		t.Fatalf("status %d, code %q", res.StatusCode, body.Code)
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err = client.Get(srv.URL + "/" + body.Code)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusFound || res.Header.Get("Location") != "https://example.com/x" {
		t.Errorf("status %d, location %q", res.StatusCode, res.Header.Get("Location"))
	}
}

func TestBadURL(t *testing.T) {
	srv := httptest.NewServer(New(store.NewMemory()))
	defer srv.Close()
	res, err := http.Post(srv.URL+"/shorten", "application/json", strings.NewReader(`{"url":"ftp://x"}`))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Errorf("status %d, want 400", res.StatusCode)
	}
}
