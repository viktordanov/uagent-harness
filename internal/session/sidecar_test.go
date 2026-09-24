package session_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/viktordanov/uagent-harness/internal/session"
)

func TestTree(t *testing.T) {
	infos := []session.Info{
		{ID: "child", Parent: "parent", Source: session.SourceSubagent},
		{ID: "other"},
		{ID: "parent", Source: session.SourceTUI},
		{ID: "orphan", Parent: "gone", Source: session.SourceSubagent},
		{ID: "grandchild", Parent: "child", Source: session.SourceSubagent},
	}

	var got []string
	var depths []int
	for _, n := range session.Tree(infos) {
		got = append(got, n.ID)
		depths = append(depths, n.Depth)
	}

	assert.Equal(t, []string{"other", "parent", "child", "grandchild", "orphan"}, got)
	assert.Equal(t, []int{0, 0, 1, 2, 0}, depths)
	assert.Len(t, session.Interactive(infos), 2, "the picker hides subagents")
}

func TestShortID(t *testing.T) {
	assert.Equal(t, "1a2b3c4d", session.ShortID("1a2b3c4d-0000-4000-8000-000000000000"))
	assert.Equal(t, "subagent-1a2b3c4d", session.ShortID("subagent-1a2b3c4d-0000-4000-8000-000000000000"))
	assert.Equal(t, "abc", session.ShortID("abc"))
	id := session.NewSubagentID()
	assert.Regexp(t, `^subagent-[0-9a-f-]{36}$`, id)
	assert.True(t, strings.HasPrefix(id, session.ShortID(id)), "the short ID is a prefix, so it resumes")
}
