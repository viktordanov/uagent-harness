package agents_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uah/internal/agents"
	"github.com/viktordanov/uah/internal/engine/embedded"
	"github.com/viktordanov/uah/testing/fakellm"
)

func freeformPatch(file, line string) fakellm.Call {
	return fakellm.Call{Name: "apply_patch", Args: "*** Begin Patch\n*** Add File: " + file + "\n+" + line + "\n*** End Patch\n", Custom: true}
}

// itemTypes are the types of a request's input items.
func itemTypes(t *testing.T, req fakellm.Request) []string {
	t.Helper()
	var out []string
	for _, raw := range req.Input {
		var it struct {
			Type string `json:"type"`
		}
		require.NoError(t, json.Unmarshal(raw, &it))
		out = append(out, it.Type)
	}

	return out
}

// TestFreeform_ForkedChildKeepsTheParentsPatch: under the freeform-patch
// experiment a forked child is offered the freeform apply_patch, gets the
// parent's freeform call and its output as they were sent, and applies its
// own freeform patch.
func TestFreeform_ForkedChildKeepsTheParentsPatch(t *testing.T) {
	e := newEnv(t, agents.Config{},
		fakellm.Reply{Calls: []fakellm.Call{freeformPatch("parent.txt", "from the parent")}},
		fakellm.Reply{Calls: []fakellm.Call{call("spawn_agent", `{"message":"CHILD-FORK go on","fork_context":true}`)}},
		callWith("wait_agent", `{"targets":["ID"]}`),
		fakellm.Reply{Text: "done"},
	)
	e.llm.Route("CHILD-FORK",
		fakellm.Reply{Calls: []fakellm.Call{freeformPatch("child.txt", "from the child")}},
		fakellm.Reply{Text: "child done"},
	)
	s, ev := e.open(t, false, func(c *embedded.Config) {
		getenv := c.Getenv
		c.Getenv = func(key string) string {
			if key == "UAH_EXPERIMENTS" {
				return "freeform-patch"
			}

			return getenv(key)
		}
	})
	_, err := s.Submit("edit, then delegate")
	require.NoError(t, err)
	assert.Equal(t, "done", ev.finished().Answer)

	child := requestWith(t, e, "CHILD-FORK")
	assert.Contains(t, itemTypes(t, child), "custom_tool_call", "the parent's call, as it was sent")
	assert.Contains(t, itemTypes(t, child), "custom_tool_call_output")
	var offered string
	for _, raw := range child.ToolDefs {
		if strings.Contains(string(raw), `"name":"apply_patch"`) {
			offered = string(raw)
		}
	}
	assert.Contains(t, offered, `"type":"custom"`)
	for file, want := range map[string]string{"parent.txt": "from the parent\n", "child.txt": "from the child\n"} {
		data, err := os.ReadFile(filepath.Join(e.Workspace, file))
		require.NoError(t, err)
		assert.Equal(t, want, string(data))
	}
	assert.Contains(t, childOutputs(e, "CHILD-FORK"), "Success. Updated the following files:\nA child.txt")
}
