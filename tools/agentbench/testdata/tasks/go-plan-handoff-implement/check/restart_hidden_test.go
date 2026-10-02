package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The agentbench check: a link saved before a restart still redirects
// after it with -data, and is forgotten without.
func TestHiddenRestart(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "shorty")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	data := filepath.Join(dir, "links.db")
	start := func(args ...string) (string, func()) {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		addr := l.Addr().String()
		l.Close()
		cmd := exec.Command(bin, append([]string{"-addr", addr}, args...)...)
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		for i := 0; ; i++ {
			if res, err := http.Get("http://" + addr + "/healthz"); err == nil {
				res.Body.Close()

				break
			}
			if i > 100 {
				t.Fatal("shorty did not start")
			}
			time.Sleep(50 * time.Millisecond)
		}

		return "http://" + addr, func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }
	}
	shorten := func(base, u string) string {
		res, err := http.Post(base+"/shorten", "application/json", strings.NewReader(fmt.Sprintf(`{"url":%q}`, u)))
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var body struct{ Code string }
		if err := json.NewDecoder(res.Body).Decode(&body); err != nil || res.StatusCode != http.StatusCreated {
			t.Fatalf("shorten: status %d, %v", res.StatusCode, err)
		}

		return body.Code
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	location := func(base, code string) string {
		res, err := client.Get(base + "/" + code)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusFound {
			return fmt.Sprintf("status %d", res.StatusCode)
		}

		return res.Header.Get("Location")
	}

	base, stop := start("-data", data)
	a := shorten(base, "https://example.com/a")
	b := shorten(base, "https://example.com/b?q=1")
	stop() // killed, not shut down: each link must be on disk already
	base, stop = start("-data", data)
	if got := location(base, a); got != "https://example.com/a" {
		t.Errorf("after a restart, %s goes to %q", a, got)
	}
	if got := location(base, b); got != "https://example.com/b?q=1" {
		t.Errorf("after a restart, %s goes to %q", b, got)
	}
	c := shorten(base, "https://example.com/c")
	stop()
	base, stop = start("-data", data)
	if got := location(base, c); got != "https://example.com/c" {
		t.Errorf("after a second restart, %s goes to %q", c, got)
	}
	stop()
	base, stop = start()
	defer stop()
	if got := location(base, a); got != "status 404" {
		t.Errorf("without -data, %s is %q, want 404", a, got)
	}
}
