package session_test

import (
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
