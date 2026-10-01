// The tool's description and grammar are verbatim from openai/codex
// rust-v0.159.1 (Apache License 2.0, Copyright 2025 OpenAI):
// codex-rs/core/src/tools/handlers/apply_patch_spec.rs and
// codex-rs/core/assets/tools/apply_patch.lark.

package patch

import (
	_ "embed"
	"encoding/json"
	"errors"
	"strings"
)

// ToolName is the tool's name, as Codex offers it.
const ToolName = "apply_patch"

// HookAliases are the other tool names a hook matcher may use for
// apply_patch, as Codex's hooks accept (HookToolName::apply_patch in
// codex-rs/core/src/tools/hook_names.rs).
var HookAliases = []string{"Edit", "Write"}

// Description is the tool's description, as Codex offers it
// (create_apply_patch_freeform_tool).
const Description = "The `apply_patch` tool can be used to edit files. This is a FREEFORM tool, so do not wrap the patch in JSON."

// Grammar is the Lark grammar the provider samples a call's patch from.
//
//go:embed apply_patch.lark
var Grammar string

// ErrNoInput is arguments without a patch.
var ErrNoInput = errors.New("the input must be a patch that starts with *** Begin Patch")

// ParseArgs reads a call's patch: its input, which is the patch itself, or
// the {"input": patch} of a call recorded when apply_patch was a function
// tool.
func ParseArgs(arguments string) (string, error) {
	if strings.HasPrefix(strings.TrimSpace(arguments), beginPatch) {
		return arguments, nil
	}
	var a struct {
		Input string `json:"input"`
	}
	if err := json.Unmarshal([]byte(arguments), &a); err != nil || strings.TrimSpace(a.Input) == "" {
		return "", ErrNoInput
	}

	return a.Input, nil
}

// hookInput is what hooks see as tool_input: Codex's {"command": patch},
// with the files the patch names, as Claude Code's Edit gives file_path.
type hookInput struct {
	Command   string   `json:"command"`
	FilePath  string   `json:"file_path,omitempty"`
	FilePaths []string `json:"file_paths,omitempty"`
}

// HookInput turns the tool's arguments into a hook's tool_input, or
// returns them unchanged when they hold no patch.
func HookInput(arguments string) json.RawMessage {
	patch, err := ParseArgs(arguments)
	if err != nil {
		return json.RawMessage(arguments)
	}
	in := hookInput{Command: patch}
	if hunks, err := Parse(patch); err == nil {
		for _, h := range hunks {
			in.FilePaths = append(in.FilePaths, h.Target())
		}
		if len(in.FilePaths) > 0 {
			in.FilePath = in.FilePaths[0]
		}
	}
	data, err := json.Marshal(in)
	if err != nil {
		return json.RawMessage(arguments)
	}

	return data
}

// FromHookInput turns a hook's updatedInput back into the tool's input:
// its "command" (or "input") is the new patch.
func FromHookInput(updated json.RawMessage) (string, error) {
	var in struct {
		Command string `json:"command"`
		Input   string `json:"input"`
	}
	if err := json.Unmarshal(updated, &in); err != nil {
		return "", ErrNoInput
	}
	patch := in.Command
	if patch == "" {
		patch = in.Input
	}
	if patch == "" {
		return "", ErrNoInput
	}

	return patch, nil
}

// Describe names the files a call's patch changes, for a tool line
// ("a.go, b.go"), or returns "" when the arguments hold no valid patch.
func Describe(arguments string) string {
	patch, err := ParseArgs(arguments)
	if err != nil {
		return ""
	}
	hunks, err := Parse(patch)
	if err != nil {
		return ""
	}
	names := make([]string, 0, len(hunks))
	for _, h := range hunks {
		names = append(names, h.Target())
	}

	return strings.Join(names, ", ")
}
