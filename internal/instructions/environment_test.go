package instructions_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/viktordanov/uagent-harness/internal/instructions"
)

// TestEnvironment renders Codex's <environment_context> for one local
// environment, as codex-rs/core/src/context/world_state/
// environment_render_tests.rs expects it at rust-v0.156.1.
func TestEnvironment(t *testing.T) {
	env := instructions.Environment{Cwd: "/repo", Shell: "bash", CurrentDate: "2026-02-26", Timezone: "America/Los_Angeles"}
	assert.Equal(t, `<environment_context>
  <cwd>/repo</cwd>
  <shell>bash</shell>
  <current_date>2026-02-26</current_date>
  <timezone>America/Los_Angeles</timezone>
</environment_context>`, env.String())

	env = instructions.Environment{Cwd: `/a&b/<c>"'`}
	assert.Equal(t, "<environment_context>\n  <cwd>/a&amp;b/&lt;c&gt;&quot;&apos;</cwd>\n</environment_context>", env.String(),
		"values are escaped, and empty ones left out")
}

// TestLocalEnvironment reads the shell's name and the time zone from TZ,
// else /etc/localtime; the date is local to that zone, or UTC in Etc/UTC
// when no zone is known, as in Codex.
func TestLocalEnvironment(t *testing.T) {
	now := time.Date(2026, 9, 29, 23, 30, 0, 0, time.UTC)
	tz := func(v string) func(string) string {
		return func(k string) string {
			if k == "TZ" {
				return v
			}

			return ""
		}
	}

	env := instructions.LocalEnvironment("/w", "/opt/homebrew/bin/fish", now, tz("Asia/Tokyo"))
	assert.Equal(t, instructions.Environment{Cwd: "/w", Shell: "fish", CurrentDate: "2026-09-30", Timezone: "Asia/Tokyo"}, env)
	env = instructions.LocalEnvironment("/w", "/bin/zsh", now, tz(":/usr/share/zoneinfo/America/New_York"))
	assert.Equal(t, "America/New_York", env.Timezone, "a TZ path names its zone")
	assert.Equal(t, "2026-09-29", env.CurrentDate)
	env = instructions.LocalEnvironment("/w", "/bin/zsh", now.Add(time.Hour), tz("Not/AZone"))
	assert.Equal(t, "Etc/UTC", env.Timezone, "an unknown zone falls back to UTC")
	assert.Equal(t, "2026-09-30", env.CurrentDate)
}
