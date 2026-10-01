package config

import (
	"strings"
	"testing"
)

func TestRetriesHidden(t *testing.T) {
	c, err := Load(nil, env(nil))
	if err != nil || c.Retries != 3 {
		t.Fatalf("default: %+v, %v", c, err)
	}
	c, err = Load(strings.NewReader("retries = 7\n"), env(nil))
	if err != nil || c.Retries != 7 {
		t.Fatalf("file: %+v, %v", c, err)
	}
	c, err = Load(strings.NewReader("retries = 7\n"), env(map[string]string{"FETCHER_RETRIES": "0"}))
	if err != nil || c.Retries != 0 {
		t.Fatalf("env: %+v, %v", c, err)
	}
	for _, bad := range []string{"-1", "many", "2.5"} {
		if _, err := Load(strings.NewReader("retries = "+bad+"\n"), env(nil)); err == nil {
			t.Errorf("file retries = %s accepted", bad)
		}
		if _, err := Load(nil, env(map[string]string{"FETCHER_RETRIES": bad})); err == nil {
			t.Errorf("FETCHER_RETRIES=%s accepted", bad)
		}
	}
}
