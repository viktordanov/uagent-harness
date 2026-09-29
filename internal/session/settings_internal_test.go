package session

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/viktordanov/uagent/core"
)

// TestSettings_WithRequestInvertsRequest keeps WithRequest in step with
// request: every field a request carries comes back.
func TestSettings_WithRequestInvertsRequest(t *testing.T) {
	s := Settings{
		Provider: "openai", Model: "m", Effort: "low", ServiceTier: "priority", Workspace: "/w", BaseURL: "http://x",
		AllowDotenv: true, SystemPrompt: "p", Sandbox: "read-only", ContextWindow: 7, MaxAttempts: 10,
	}
	assert.Equal(t, s, Settings{ServiceTier: "priority", Sandbox: "read-only", ContextWindow: 7}.WithRequest(s.request("id", nil)))
}

// TestSettings_NoTimeoutReachesAChild pins that a subagent's run has no
// wall-clock limit, whatever the parent's request carried.
func TestSettings_NoTimeoutReachesAChild(t *testing.T) {
	parent := core.Request{Provider: "openai", Model: "m", Workspace: "/w", Timeout: time.Hour}

	child := Settings{}.WithRequest(parent).request("child", nil)

	assert.Zero(t, child.Timeout)
}
