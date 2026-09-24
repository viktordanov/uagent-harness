package main

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/viktordanov/uagent/core"
	uaharness "github.com/viktordanov/uagent/harness"

	"github.com/viktordanov/uagent-harness/internal/engine"
	"github.com/viktordanov/uagent-harness/internal/patch"
	"github.com/viktordanov/uagent-harness/internal/session"
)

// TestPrintTranscript_Diff prints an applied patch as plain +/- lines.
func TestPrintTranscript_Diff(t *testing.T) {
	at := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	args := `{"input":"*** Begin Patch\n*** Update File: a.go\n@@\n-x\n+y\n*** End Patch"}`
	run := session.LoadedRun{
		Record: uaharness.RunRecord{Result: core.Result{Request: core.Request{RunID: "r1"}, Status: core.StatusOK, StartedAt: at}},
		Events: []core.Event{
			core.ToolCalled{At: at, CallID: "c1", Name: "apply_patch", Label: args, Arguments: args},
			engine.PatchApplied{At: at, CallID: "c1", Files: []patch.FileDiff{{
				Op: "update", Path: "a.go", Added: 1, Removed: 1,
				Hunks: []patch.DiffHunk{{Lines: []patch.DiffLine{
					{Kind: " ", Old: 1, New: 1, Text: "package a"},
					{Kind: "-", Old: 2, Text: "x"},
					{Kind: "+", New: 2, Text: "y"},
				}}},
			}}},
		},
	}
	var out bytes.Buffer
	printTranscript(&out, session.Info{ID: "s1", Provider: "openai", Model: "gpt-5.5", Workspace: "/w"}, []session.LoadedRun{run})

	assert.Contains(t, out.String(), "  → apply_patch  a.go\n"+
		"    Edited a.go (+1 -1)\n"+
		"         1  package a\n"+
		"         2 -x\n"+
		"         2 +y\n")
}
