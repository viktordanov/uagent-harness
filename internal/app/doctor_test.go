package app_test

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent-harness/internal/app"
	"github.com/viktordanov/uagent-harness/internal/hooks"
	"github.com/viktordanov/uagent-harness/testing/harnesstest"
)

// find returns the check with the name, failing the test without one.
func find(t *testing.T, checks []app.Check, name string) app.Check {
	t.Helper()
	for _, c := range checks {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("no %q check in %+v", name, checks)

	return app.Check{}
}

func TestDoctor_Healthy(t *testing.T) {
	_, in := setupEnv(t)
	checks := app.Doctor(context.Background(), in, app.DoctorOptions{HookTrustFile: filepath.Join(t.TempDir(), "trust.json")})

	names := make([]string, 0, len(checks))
	for _, c := range checks {
		names = append(names, c.Name)
		if c.Name == "sandbox" && c.Status == app.CheckWarn {
			continue // no sandbox on this machine
		}
		assert.Equal(t, app.CheckOK, c.Status, "%s: %s", c.Name, c.Detail)
	}
	assert.Equal(t, []string{"config", "runner", "workspace", "credentials", "sandbox", "instructions", "hooks", "mcp", "state"}, names)
	assert.True(t, app.Healthy(checks))
	assert.Contains(t, find(t, checks, "credentials").Detail, "openai-codex credentials found")
}

func TestDoctor_Problems(t *testing.T) {
	cases := map[string]struct {
		prepare func(t *testing.T, e *harnesstest.Env, in *app.Inputs, opts *app.DoctorOptions)
		check   string
		status  app.CheckStatus
		detail  string
	}{
		"a broken config file": {
			prepare: func(t *testing.T, _ *harnesstest.Env, in *app.Inputs, _ *app.DoctorOptions) {
				writeConfig(t, in, "model = \n")
			},
			check: "config", status: app.CheckFail, detail: "config.toml",
		},
		"an invalid config value": {
			prepare: func(t *testing.T, _ *harnesstest.Env, in *app.Inputs, _ *app.DoctorOptions) {
				writeConfig(t, in, "sandbox_mode = \"bogus\"\n")
			},
			check: "settings", status: app.CheckFail, detail: "invalid sandbox mode",
		},
		"an untrusted project config": {
			prepare: func(t *testing.T, e *harnesstest.Env, _ *app.Inputs, _ *app.DoctorOptions) {
				writeFile(t, filepath.Join(e.Workspace, ".uagent", "config.toml"), "effort = \"low\"\n")
			},
			check: "project config", status: app.CheckWarn, detail: "not trusted",
		},
		"no runner for the process engine": {
			prepare: func(_ *testing.T, _ *harnesstest.Env, in *app.Inputs, _ *app.DoctorOptions) {
				in.Engine, in.Runner = app.EngineProcess, "/nonexistent/unreal-agent-runner"
			},
			check: "runner", status: app.CheckFail, detail: "not an executable file",
		},
		"an expired Codex token": {
			prepare: func(t *testing.T, e *harnesstest.Env, _ *app.Inputs, _ *app.DoctorOptions) {
				writeToken(t, e, -time.Hour)
			},
			check: "credentials", status: app.CheckFail, detail: "expired",
		},
		"a Codex token about to expire": {
			prepare: func(t *testing.T, e *harnesstest.Env, _ *app.Inputs, _ *app.DoctorOptions) {
				writeToken(t, e, 10*time.Minute)
			},
			check: "credentials", status: app.CheckWarn, detail: "expires in",
		},
		"no API key": {
			prepare: func(_ *testing.T, _ *harnesstest.Env, in *app.Inputs, opts *app.DoctorOptions) {
				in.Provider, in.Model = "openai", "gpt-test"
				opts.Getenv = func(string) string { return "" }
			},
			check: "credentials", status: app.CheckFail, detail: "OPENAI_API_KEY",
		},
		"state inside the workspace": {
			prepare: func(_ *testing.T, e *harnesstest.Env, in *app.Inputs, _ *app.DoctorOptions) {
				in.StateDir = filepath.Join(e.Workspace, "state")
			},
			check: "workspace", status: app.CheckFail, detail: "inside the workspace",
		},
		"no sandbox": {
			prepare: func(_ *testing.T, _ *harnesstest.Env, in *app.Inputs, _ *app.DoctorOptions) {
				in.Sandbox = "danger-full-access"
			},
			check: "sandbox", status: app.CheckWarn, detail: "without a sandbox",
		},
		"an unwritable state dir": {
			prepare: func(t *testing.T, e *harnesstest.Env, in *app.Inputs, _ *app.DoctorOptions) {
				in.StateDir = filepath.Join(e.StateDir, "file")
				writeFile(t, in.StateDir, "not a directory")
			},
			check: "state", status: app.CheckFail,
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			e, in := setupEnv(t)
			opts := app.DoctorOptions{HookTrustFile: filepath.Join(t.TempDir(), "trust.json")}
			c.prepare(t, e, &in, &opts)
			checks := app.Doctor(context.Background(), in, opts)
			got := find(t, checks, c.check)
			assert.Equal(t, c.status, got.Status, got.Detail)
			assert.Contains(t, got.Detail, c.detail)
			assert.NotEmpty(t, got.Fix)
			assert.Equal(t, c.status != app.CheckFail, app.Healthy(checks))
		})
	}
}

// TestDoctor_Hooks lists project hooks that do not run, with the reason.
func TestDoctor_Hooks(t *testing.T) {
	e, in := setupEnv(t)
	writeConfig(t, &in, "[projects.\""+e.Workspace+"\"]\ntrusted = true\n\n[[hooks.Stop]]\ncommand = \"true\"\n")
	writeFile(t, filepath.Join(e.Workspace, ".uagent", "config.toml"),
		"[[hooks.Stop]]\ncommand = \"./check.sh\"\n\n[[hooks.Stop]]\ncommand = \"echo new\"\n")
	writeFile(t, filepath.Join(e.Workspace, "check.sh"), "exit 0\n")
	trustFile := filepath.Join(t.TempDir(), "trust.json")
	trust, err := hooks.LoadTrust(trustFile)
	require.NoError(t, err)
	require.NoError(t, trust.Allow(e.Workspace, "./check.sh"))
	writeFile(t, filepath.Join(e.Workspace, "check.sh"), "exit 0 # changed\n")

	checks := app.Doctor(context.Background(), in, app.DoctorOptions{HookTrustFile: trustFile})
	assert.Equal(t, "3 configured: 1 user, 2 project (0 trusted)", find(t, checks, "hooks").Detail)
	var untrusted []string
	for _, c := range checks {
		if c.Name == "hook" {
			assert.Equal(t, app.CheckWarn, c.Status)
			assert.Contains(t, c.Fix, "uah hooks trust")
			untrusted = append(untrusted, c.Detail)
		}
	}
	require.Len(t, untrusted, 2)
	assert.Contains(t, untrusted[0], "the script changed")
	assert.Contains(t, untrusted[1], "not trusted")
	assert.True(t, app.Healthy(checks), "untrusted hooks are warnings")
}

// TestDoctor_MCP starts each server and reports its tools, or why it failed.
func TestDoctor_MCP(t *testing.T) {
	_, in := setupEnv(t)
	writeConfig(t, &in, "[mcp_servers.test]\ncommand = \""+harnesstest.MCPServer(t)+"\"\n\n"+
		"[mcp_servers.broken]\ncommand = \"/nonexistent/server\"\nstartup_timeout_sec = 2\n")

	checks := app.Doctor(context.Background(), in, app.DoctorOptions{HookTrustFile: filepath.Join(t.TempDir(), "trust.json")})
	good := find(t, checks, "mcp test")
	assert.Equal(t, app.CheckOK, good.Status, good.Detail)
	assert.Regexp(t, `^started, \d+ tools \(startup timeout 30s\)$`, good.Detail)
	bad := find(t, checks, "mcp broken")
	assert.Equal(t, app.CheckFail, bad.Status)
	assert.Contains(t, bad.Detail, "startup timeout 2s")
	assert.False(t, app.Healthy(checks))
}

func writeConfig(t *testing.T, in *app.Inputs, content string) {
	t.Helper()
	writeFile(t, in.ConfigPath, content)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

// writeToken replaces the Codex token with one that expires in d.
func writeToken(t *testing.T, e *harnesstest.Env, d time.Duration) {
	t.Helper()
	claims := `{"exp":` + strconv.FormatInt(time.Now().Add(d).Unix(), 10) + `,"https://api.openai.com/auth":{"chatgpt_account_id":"a"}}`
	writeFile(t, filepath.Join(e.CodexHome, "auth.json"), `{"tokens":{"access_token":"x.`+base64.RawURLEncoding.EncodeToString([]byte(claims))+`.y"}}`)
}
