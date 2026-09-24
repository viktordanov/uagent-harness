package session

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestSettings_WithRequestInvertsRequest keeps WithRequest in step with
// request: every field a request carries comes back.
func TestSettings_WithRequestInvertsRequest(t *testing.T) {
	s := Settings{
		Provider: "openai", Model: "m", Effort: "low", ServiceTier: "priority", Workspace: "/w", BaseURL: "http://x",
		Timeout: time.Minute, AllowDotenv: true, SystemPrompt: "p", Sandbox: "read-only", ContextWindow: 7, MaxAttempts: 10,
	}
	assert.Equal(t, s, Settings{ServiceTier: "priority", Sandbox: "read-only", ContextWindow: 7}.WithRequest(s.request("id", nil)))
}
