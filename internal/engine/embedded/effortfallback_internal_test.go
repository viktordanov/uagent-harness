package embedded

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uah-core/harness/llm/responsesapi"

	"github.com/viktordanov/uah/internal/engine"
)

func TestRejectionOf(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
		want rejection
	}{
		{"the item's type in the message", &responsesapi.APIError{StatusCode: 400, Code: "invalid_value", Message: "Invalid value: 'configuration_update'.", Param: "input[2].type"}, rejected},
		{"the ChatGPT backend's detail", &responsesapi.APIError{StatusCode: 400, Message: `{"detail":"Unsupported input item type: configuration_update"}`}, rejected},
		{"a failed stream naming it", &responsesapi.APIError{StatusCode: 200, Code: "invalid_request", Message: "configuration update not supported"}, rejected},
		{"wrapped", fmt.Errorf("create response: %w", &responsesapi.APIError{StatusCode: 422, Message: "unknown item configuration_update"}), rejected},
		{"an invalid input", &responsesapi.APIError{StatusCode: 400, Code: "invalid_value", Message: "Invalid input.", Param: "input[3]"}, maybeRejected},
		{"a bare 400", &responsesapi.APIError{StatusCode: 400, Message: "Bad Request"}, maybeRejected},
		{"a context too long", &responsesapi.APIError{StatusCode: 400, Code: "context_length_exceeded", Message: "Your input exceeds the context window.", Param: "input"}, notRejected},
		{"an invalid tool", &responsesapi.APIError{StatusCode: 400, Code: "invalid_value", Message: "Invalid tool.", Param: "tools[0].name"}, notRejected},
		{"an invalid effort", &responsesapi.APIError{StatusCode: 400, Code: "unsupported_value", Message: "Unsupported value: 'xhigh'.", Param: "reasoning.effort"}, notRejected},
		{"an expired login", &responsesapi.APIError{StatusCode: 401, Message: "Unauthorized"}, notRejected},
		{"a rate limit", &responsesapi.APIError{StatusCode: 429, Code: "rate_limit_exceeded", Message: "slow down"}, notRejected},
		{"a server error naming it", &responsesapi.APIError{StatusCode: 500, Message: "configuration_update handler crashed"}, notRejected},
		{"not the API's", errors.New("connection reset"), notRejected},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, rejectionOf(tt.err))
		})
	}
}

// TestUpdatesRejected_Fork: the file turns a session's updates off, and a
// fork inherits it.
func TestUpdatesRejected_Fork(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	off, err := updatesRejected(dir, "parent")
	require.NoError(t, err)
	assert.False(t, off)
	require.NoError(t, copyUpdatesRejected(dir, "parent", "none"), "nothing to copy")

	require.NoError(t, saveUpdatesRejected(dir, "parent", engine.EffortUpdatesOff{At: time.Now(), Err: "rejected"}))
	require.NoError(t, copyUpdatesRejected(dir, "parent", "child"))

	for _, id := range []string{"parent", "child"} {
		off, err := updatesRejected(dir, id)
		require.NoError(t, err)
		assert.True(t, off, id)
	}
	off, err = updatesRejected(dir, "none")
	require.NoError(t, err)
	assert.False(t, off)
}
