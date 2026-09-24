package app_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/viktordanov/uagent-harness/internal/app"
	"github.com/viktordanov/uagent-harness/testing/harnesstest"
)

// TestDoctor_Engine reports what the engine does not run, and warns once
// for each configured feature among those, with the session's notice.
func TestDoctor_Engine(t *testing.T) {
	config := "[mcp_servers.docs]\ncommand = \"true\"\n\n[[hooks.PreToolUse]]\ncommand = \"true\"\n\n[approvals]\nforbid = [\"rm\"]\n"
	cases := map[string]struct {
		engine, gate, config string
		status               app.CheckStatus
		detail               []string
	}{
		"embedded": {engine: app.EngineEmbedded, config: config, status: app.CheckOK, detail: []string{"embedded: runs every feature"}},
		"process, nothing configured": {
			engine: app.EngineProcess, gate: "/usr/bin/uah", status: app.CheckOK,
			detail: []string{"process, without live input, live settings", "; nothing configured needs them"},
		},
		"process with MCP and PreToolUse hooks": {
			engine: app.EngineProcess, gate: "/usr/bin/uah", config: config, status: app.CheckWarn,
			detail: []string{
				"PreToolUse hooks: not supported by the process engine (they do not run); use the embedded engine; " +
					"MCP servers: not supported by the process engine (they do not start); use the embedded engine",
			},
		},
		"process without the gate": {
			engine: app.EngineProcess, config: "[approvals]\nforbid = [\"rm\"]\n", status: app.CheckWarn,
			detail: []string{"command rules: not supported by the process engine (no command rule applies)"},
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, in := setupEnv(t)
			in.Engine, in.Runner, in.Gate = c.engine, harnesstest.FakeRunner(t), c.gate
			writeConfig(t, &in, c.config)
			got := find(t, app.Doctor(context.Background(), in, doctorOptions(t, filepath.Join(t.TempDir(), "trust.json"))), "engine")
			assert.Equal(t, c.status, got.Status, got.Detail)
			for _, d := range c.detail {
				assert.Contains(t, got.Detail, d)
			}
			if c.gate != "" {
				assert.NotContains(t, got.Detail, "command rules", "the gate applies them")
			}
		})
	}
}
