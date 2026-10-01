package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "inv.txt")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	return p
}

func TestText(t *testing.T) {
	p := writeFile(t, "pear,2,0.5\napple,3,1.20\n")
	var out, errb bytes.Buffer
	if code := Run([]string{p}, &out, &errb); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	want := "NAME   QTY  PRICE\napple  3    1.20\npear   2    0.50\n"
	if out.String() != want {
		t.Fatalf("got\n%q\nwant\n%q", out.String(), want)
	}
}

func TestBadSort(t *testing.T) {
	var out, errb bytes.Buffer
	if code := Run([]string{"-sort", "price", writeFile(t, "")}, &out, &errb); code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
}
