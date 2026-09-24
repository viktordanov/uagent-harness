package agents

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestReadable(t *testing.T) {
	for in, want := range map[string]string{
		`the coordinator stopped: call model for turn t1: create response: responses API request failed with status 400: {"detail":"The 'gpt-luna-6' model is not supported."}`: "The 'gpt-luna-6' model is not supported.",
		`status 400: {"error":{"code":"x","message":"bad request"}}`: "bad request",
		`status 500: {"error":"overloaded"}`:                         "overloaded",
		"plain\nerror   text":                                        "plain error text",
		`{"other":1} then`:                                           `{"other":1} then`,
	} {
		assert.Equal(t, want, readable(in), in)
	}
	assert.Len(t, []rune(readable(strings.Repeat("x", 2000))), maxCause)
}
