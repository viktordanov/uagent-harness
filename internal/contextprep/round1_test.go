package contextprep_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uah/internal/contextprep"
)

// round1Case is a block as round 1's code wrote it, before the text moved
// into modules (testdata/round1.json, recorded from that code).
type round1Case struct {
	Name             string   `json:"name"`
	Shell            string   `json:"shell"`
	GOOS             string   `json:"goos"`
	Mode             string   `json:"mode"`
	Network          bool     `json:"network"`
	TmpDir           string   `json:"tmpdir"`
	InstructionFiles []string `json:"instruction_files"`
	Text             string   `json:"text"`
}

func (c round1Case) facts() contextprep.Facts {
	return contextprep.Facts{
		Shell: c.Shell, GOOS: c.GOOS, InstructionFiles: c.InstructionFiles, MaxOutputLength: 40000,
		Sandbox: contextprep.Sandbox{Mode: c.Mode, Network: c.Network, TempDir: c.TmpDir},
	}
}

// fixedBlock is a block with a fixed text.
type fixedBlock struct{ name, text string }

func (b fixedBlock) Name() string                                      { return b.name }
func (b fixedBlock) Prepare(context.Context, contextprep.Facts) string { return b.text }

// TestDefaultsMatchRound1: with the built-in modules alone, each block, and
// the whole prepared message, is byte for byte what round 1's code wrote,
// for each shell, OS, and sandbox mode recorded.
func TestDefaultsMatchRound1(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("testdata/round1.json")
	require.NoError(t, err)
	var cases []round1Case
	require.NoError(t, json.Unmarshal(data, &cases))
	require.Greater(t, len(cases), 100)
	ms := contextprep.Load(contextprep.Sources{})
	blocks := map[string]contextprep.Adapter{
		"environment": contextprep.Environment{Modules: ms}, "sandbox": contextprep.SandboxNotes{Modules: ms},
		"agent files": contextprep.AgentFiles{Modules: ms}, "harness": contextprep.Harness{Modules: ms},
	}
	byName := map[string][]round1Case{}
	for _, c := range cases {
		assert.Equal(t, c.Text, blocks[c.Name].Prepare(t.Context(), c.facts()), "%s: %+v", c.Name, c)
		byName[c.Name] = append(byName[c.Name], c)
	}

	// The whole message for a session with every block: the round-1 texts
	// joined as Prepare joins blocks, against the modules' message.
	files, harness := byName["agent files"][len(byName["agent files"])-1], byName["harness"][0]
	notRepo := t.TempDir() // no workspace block
	for _, sb := range byName["sandbox"] {
		var env *round1Case
		for i, e := range byName["environment"] {
			if e.Shell == sb.Shell && e.GOOS == sb.GOOS {
				env = &byName["environment"][i]
			}
		}
		require.NotNil(t, env)
		f := sb.facts()
		f.InstructionFiles, f.Workspace = files.InstructionFiles, notRepo
		want := contextprep.Prepare(t.Context(), f, fixedBlock{"environment", env.Text}, fixedBlock{"sandbox", sb.Text},
			fixedBlock{"agent files", files.Text}, fixedBlock{"harness", harness.Text})
		assert.Equal(t, want, contextprep.Prepare(t.Context(), f, ms.Adapters()...), "%+v", sb)
	}
}
