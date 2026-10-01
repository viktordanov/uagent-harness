package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func files(t *testing.T) (a, b string) {
	t.Helper()
	dir := t.TempDir()
	a, b = filepath.Join(dir, "a.txt"), filepath.Join(dir, "b.txt")
	if err := os.WriteFile(a, []byte("1\n2\n3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	return a, b
}

func call(args ...string) (int, string, string) {
	var out, errb bytes.Buffer
	code := run(args, &out, &errb)

	return code, out.String(), errb.String()
}

func TestOK(t *testing.T) {
	a, b := files(t)
	code, out, errs := call(a, b)
	if code != 0 || out != "3 "+a+"\n1 "+b+"\n4 total\n" || errs != "" {
		t.Fatalf("code %d out %q err %q", code, out, errs)
	}
	code, out, errs = call("-max", "3", a)
	if code != 0 || out != "3 "+a+"\n" || errs != "" {
		t.Fatalf("single file: code %d out %q err %q", code, out, errs)
	}
}

func TestMissingFile(t *testing.T) {
	a, b := files(t)
	missing := filepath.Join(filepath.Dir(a), "nope.txt")
	code, out, errs := call(a, missing, b)
	if code != 1 {
		t.Errorf("code = %d, want 1", code)
	}
	if out != "3 "+a+"\n1 "+b+"\n4 total\n" {
		t.Errorf("stdout = %q", out)
	}
	if !strings.HasPrefix(errs, "cnt: ") || !strings.Contains(errs, "nope.txt") || strings.Count(errs, "\n") != 1 {
		t.Errorf("stderr = %q", errs)
	}
}

func TestUsage(t *testing.T) {
	for _, args := range [][]string{{}, {"-bogus", "x"}, {"-max", "abc", "x"}, {"-max", "-1", "x"}} {
		code, out, errs := call(args...)
		if code != 2 || out != "" || !strings.HasPrefix(errs, "cnt: ") || !strings.HasSuffix(errs, "usage: cnt [-max N] FILE...\n") {
			t.Errorf("%q: code %d out %q err %q", args, code, out, errs)
		}
	}
}

func TestHelp(t *testing.T) {
	for _, h := range []string{"-h", "-help"} {
		code, out, errs := call(h)
		if code != 0 || out != "usage: cnt [-max N] FILE...\n" || errs != "" {
			t.Errorf("%s: code %d out %q err %q", h, code, out, errs)
		}
	}
}

func TestMax(t *testing.T) {
	a, b := files(t)
	code, out, errs := call("-max", "2", a, b)
	if code != 3 || out != "3 "+a+"\n1 "+b+"\n4 total\n" || errs != "cnt: "+a+": 3 lines exceeds -max 2\n" {
		t.Fatalf("code %d out %q err %q", code, out, errs)
	}
	code, _, _ = call("-max", "0", a, filepath.Join(t.TempDir(), "missing"))
	if code != 1 {
		t.Fatalf("missing file and limit: code %d, want 1", code)
	}
}
