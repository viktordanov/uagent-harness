package render

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestToolLabelAndText(t *testing.T) {
	for name, want := range map[string]string{
		"Bash": "RAN", "SkillUse": "SKILL", "ViewImage": "VIEW", "view_image": "VIEW",
		"wait_agent": "WAIT", "mcp__docs__search": "MCP", "Read": "READ",
	} {
		assert.Equal(t, want, toolLabel(name), name)
	}
	assert.Equal(t, "i-have-adhd", toolText(`{"name":"i-have-adhd"}`))
	assert.Equal(t, `{"a":"x","b":"y"}`, toolText(`{"a":"x","b":"y"}`), "more fields stay as given")
	assert.Equal(t, "go test ./...", toolText("go   test ./..."))
}
