package mcp

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNames(t *testing.T) {
	n := newNamer()
	assert.Equal(t, "mcp__github__create_issue", n.name("github", "create_issue"))
	assert.Equal(t, "mcp__my_server__get_page", n.name("my-server", "get.page"), "sanitized as Codex does")

	// A collision after sanitizing gets a hash suffix.
	collided := n.name("my.server", "get-page")
	assert.Regexp(t, `^mcp__my_server__get_page_[0-9a-f]{12}$`, collided)

	// A long name is cut to fit, with a hash suffix.
	long := n.name("server", strings.Repeat("x", 100))
	assert.Len(t, long, MaxNameLength)
	assert.Regexp(t, `^mcp__server__x+_[0-9a-f]{12}$`, long)

	// The same inputs name the same way in a new namer.
	assert.Equal(t, long, newNamer().name("server", strings.Repeat("x", 100)))
	assert.Equal(t, "_", Sanitize(""))
}
