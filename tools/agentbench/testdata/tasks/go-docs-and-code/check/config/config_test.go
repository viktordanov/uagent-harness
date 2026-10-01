package config

import (
	"strings"
	"testing"
	"time"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestLoad(t *testing.T) {
	c, err := Load(strings.NewReader("timeout = 5s # short\nuser_agent = x\n"), env(map[string]string{"FETCHER_USER_AGENT": "y"}))
	if err != nil {
		t.Fatal(err)
	}
	if c.Timeout != 5*time.Second || c.UserAgent != "y" {
		t.Fatalf("got %+v", c)
	}
}

func TestUnknownKey(t *testing.T) {
	if _, err := Load(strings.NewReader("colour = red\n"), env(nil)); err == nil {
		t.Fatal("unknown key accepted")
	}
}
