package ini

import (
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
