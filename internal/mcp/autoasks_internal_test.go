package mcp

import (
	"testing"

	"github.com/stretchr/testify/assert"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestAutoAsks(t *testing.T) {
	yes, no := true, false
	cases := map[string]struct {
		a    *sdk.ToolAnnotations
		asks bool
	}{
		"no annotations":                  {nil, true},
		"read-only":                       {&sdk.ToolAnnotations{ReadOnlyHint: true}, false},
		"destructive wins over read-only": {&sdk.ToolAnnotations{ReadOnlyHint: true, DestructiveHint: &yes}, true},
		"safe and closed-world":           {&sdk.ToolAnnotations{DestructiveHint: &no, OpenWorldHint: &no}, false},
		"safe but open-world":             {&sdk.ToolAnnotations{DestructiveHint: &no}, true},
	}
	for name, c := range cases {
		assert.Equal(t, c.asks, autoAsks(c.a), name)
	}
}
