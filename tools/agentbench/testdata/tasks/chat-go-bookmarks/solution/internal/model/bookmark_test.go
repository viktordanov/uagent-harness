package model

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestValidate(t *testing.T) {
	ok := Bookmark{URL: "https://go.dev", Name: "Go"}
	if err := ok.Validate(); err != nil {
		t.Fatalf("valid bookmark: %v", err)
	}
	bad := []Bookmark{
		{Name: "no url"},
		{URL: "ftp://example.com", Name: "ftp"},
		{URL: "https://", Name: "no host"},
		{URL: "https://go.dev"},
		{URL: "https://go.dev", Name: strings.Repeat("x", MaxNameLen+1)},
		{URL: "https://go.dev", Name: "Go", Tags: []string{"has space"}},
	}
	for _, b := range bad {
		err := b.Validate()
		if !errors.Is(err, ErrInvalid) {
			t.Errorf("%+v: got %v, want ErrInvalid", b, err)
		}
	}
}

func TestNormalizeTags(t *testing.T) {
	got := NormalizeTags([]string{" Go ", "web", "go", "", "WEB", "api"})
	want := []string{"Go", "web", "api"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	if NormalizeTags(nil) == nil {
		t.Fatal("NormalizeTags(nil) is nil")
	}
}

func TestMatches(t *testing.T) {
	b := Bookmark{URL: "https://pkg.go.dev/net/http", Name: "net/http docs"}
	for _, q := range []string{"", "HTTP", "pkg.go", "Docs"} {
		if !b.Matches(q) {
			t.Errorf("Matches(%q) = false", q)
		}
	}
	if b.Matches("rust") {
		t.Error("Matches(rust) = true")
	}
}

func TestCloneDoesNotShareTags(t *testing.T) {
	b := Bookmark{Tags: []string{"a"}}
	c := b.Clone()
	c.Tags[0] = "b"
	if b.Tags[0] != "a" {
		t.Fatal("Clone shares the tags slice")
	}
}
