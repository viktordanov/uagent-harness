package ini

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestBasic(t *testing.T) {
	f, err := Parse(strings.NewReader("name = x\n[server]\nport = 8080\n"))
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := f.Get("", "name"); v != "x" {
		t.Errorf("name = %q", v)
	}
	if v, _ := f.Get("server", "port"); v != "8080" {
		t.Errorf("port = %q", v)
	}
}

func TestRules(t *testing.T) {
	in := strings.Join([]string{
		"; top comment",
		"Title = My App\r",
		"  # indented comment",
		"",
		"[ Server ]",
		"Host = example.com",
		"note = a # not a comment",
		`motd = "  hello \"world\" \\ \n  "`,
		`empty = ""`,
		"path = /usr/bin:\\",
		"   /bin:\\",
		"   ; not a comment",
		"plain = trailing",
		"[db]",
		"user = a",
		"[Server]",
		"HOST = override.com",
		"[db]",
		"pass = b",
		"user = c",
	}, "\n")
	f, err := Parse(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	want := File{
		"":       {"title": "My App"},
		"Server": {"host": "override.com", "note": "a # not a comment", "motd": `  hello "world" \ \n  `, "empty": "", "path": "/usr/bin: /bin: ; not a comment", "plain": "trailing"},
		"db":     {"user": "c", "pass": "b"},
	}
	if !reflect.DeepEqual(f, want) {
		t.Fatalf("got  %#v\nwant %#v", f, want)
	}
	if v, ok := f.Get("Server", "HoSt"); !ok || v != "override.com" {
		t.Errorf("Get case-insensitive key = %q, %v", v, ok)
	}
	if _, ok := f.Get("server", "host"); ok {
		t.Error("section names are case-sensitive")
	}
}

func TestErrors(t *testing.T) {
	for _, c := range []struct {
		in   string
		line int
	}{
		{"a = 1\n[broken\n", 2},
		{"[]\n", 1},
		{"[ok]\n\njust words\n", 3},
		{"= value\n", 1},
		{"a = 1\nb = \"unterminated\n", 2},
		{"x = \"quoted\" extra\n", 1},
	} {
		_, err := Parse(strings.NewReader(c.in))
		var pe *ParseError
		if !errors.As(err, &pe) {
			t.Errorf("Parse(%q) error = %v; want *ParseError", c.in, err)

			continue
		}
		if pe.Line != c.line {
			t.Errorf("Parse(%q) line = %d; want %d", c.in, pe.Line, c.line)
		}
	}
}
