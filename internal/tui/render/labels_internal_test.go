package render

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestToolLabel(t *testing.T) {
	for name, want := range map[string]string{
		"Bash": "RAN", "SkillUse": "SKILL", "ViewImage": "VIEW", "view_image": "VIEW",
		"wait_agent": "WAIT", "mcp__docs__search": "MCP", "Read": "READ",
	} {
		assert.Equal(t, want, toolLabel(name), name)
	}
}
