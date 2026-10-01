package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func noEnv(string) string { return "" }

func env(kv map[string]string) func(string) string { return func(k string) string { return kv[k] } }

func file(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "app.conf")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	return p
}

func TestDefaults(t *testing.T) {
	c, err := Load("", noEnv, nil)
	if err != nil || c != Defaults() || c.MaxConns != 10 {
		t.Fatalf("Load = %+v, %v", c, err)
	}
}

func TestPrecedence(t *testing.T) {
	p := file(t, "addr = :1\ntimeout = 1s\nmax_conns = 2\ndebug = true\n")
	c, err := Load(p, noEnv, nil)
	if err != nil || c.Addr != ":1" || c.Timeout != time.Second || c.MaxConns != 2 || !c.Debug {
		t.Fatalf("file: %+v, %v", c, err)
	}
	e := env(map[string]string{"APP_ADDR": ":2", "APP_MAX_CONNS": "3", "APP_DEBUG": "false"})
	c, err = Load(p, e, nil)
	if err != nil || c.Addr != ":2" || c.Timeout != time.Second || c.MaxConns != 3 || c.Debug {
		t.Fatalf("env: %+v, %v", c, err)
	}
	c, err = Load(p, e, []string{"-addr", ":3", "-max-conns", "4", "-timeout", "2s"})
	if err != nil || c.Addr != ":3" || c.Timeout != 2*time.Second || c.MaxConns != 4 || c.Debug {
		t.Fatalf("flags: %+v, %v", c, err)
	}
	// A flag left unset does not reset what the environment set.
	c, err = Load("", env(map[string]string{"APP_ADDR": ":5", "APP_MAX_CONNS": "7"}), []string{"-debug"})
	if err != nil || c.Addr != ":5" || c.MaxConns != 7 || !c.Debug {
		t.Fatalf("partial flags: %+v, %v", c, err)
	}
}

func TestMaxConnsErrors(t *testing.T) {
	for name, load := range map[string]func() error{
		"file":      func() error { _, err := Load(file(t, "max_conns = lots\n"), noEnv, nil); return err },
		"file zero": func() error { _, err := Load(file(t, "max_conns = 0\n"), noEnv, nil); return err },
		"env":       func() error { _, err := Load("", env(map[string]string{"APP_MAX_CONNS": "x"}), nil); return err },
		"env neg":   func() error { _, err := Load("", env(map[string]string{"APP_MAX_CONNS": "-2"}), nil); return err },
		"flag":      func() error { _, err := Load("", noEnv, []string{"-max-conns", "0"}); return err },
	} {
		err := load()
		if err == nil {
			t.Errorf("%s: no error", name)

			continue
		}
		if !strings.Contains(strings.ToLower(err.Error()), "max") {
			t.Errorf("%s: error %q does not name the key", name, err)
		}
	}
}
