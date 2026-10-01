package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func noEnv(string) string { return "" }

func TestDefaults(t *testing.T) {
	c, err := Load("", noEnv, nil)
	if err != nil || c != Defaults() {
		t.Fatalf("Load = %+v, %v", c, err)
	}
}

func TestFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "app.conf")
	if err := os.WriteFile(p, []byte("# c\naddr = :9000\ntimeout = 5s\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(p, noEnv, nil)
	if err != nil || c.Addr != ":9000" || c.Timeout != 5*time.Second {
		t.Fatalf("Load = %+v, %v", c, err)
	}
}
