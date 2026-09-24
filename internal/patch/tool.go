// The tool description is adapted from openai/codex rust-v0.156.1 (Apache
// License 2.0, Copyright 2025 OpenAI): the apply_patch section of
// codex-rs/core/gpt_5_1_prompt.md and the grammar in
// codex-rs/core/assets/tools/apply_patch.lark.

package patch

import (
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

// Description is the function tool's description: Codex's instructions for
// the patch format, and its grammar.
const Description = "Use the `apply_patch` tool to edit files. Your patch language is a stripped-down, file-oriented diff format designed to be easy to parse and safe to apply. You can think of it as a high-level envelope:\n\n" +
	"*** Begin Patch\n[ one or more file sections ]\n*** End Patch\n\n" +
	"Within that envelope, you get a sequence of file operations.\nYou MUST include a header to specify the action you are taking.\nEach operation starts with one of three headers:\n\n" +
	"*** Add File: <path> - create a new file. Every following line is a + line (the initial contents).\n" +
	"*** Delete File: <path> - remove an existing file. Nothing follows.\n" +
	"*** Update File: <path> - patch an existing file in place (optionally with a rename).\n\n" +
	"May be immediately followed by *** Move to: <new path> if you want to rename the file.\n" +
	"Then one or more \"hunks\", each introduced by @@ (optionally followed by a hunk header, such as the class or function the change is in).\n" +
	"Within a hunk each line starts with ' ' (context), '-' (removed), or '+' (added). Show about 3 lines of context above and below each change; use @@ headers when that is not enough to find the place.\n\n" +
	"The full grammar:\n" +
	"Patch := Begin { FileOp } End\nBegin := \"*** Begin Patch\" NEWLINE\nEnd := \"*** End Patch\" NEWLINE\n" +
	"FileOp := AddFile | DeleteFile | UpdateFile\nAddFile := \"*** Add File: \" path NEWLINE { \"+\" line NEWLINE }\n" +
	"DeleteFile := \"*** Delete File: \" path NEWLINE\nUpdateFile := \"*** Update File: \" path NEWLINE [ MoveTo ] { Hunk }\n" +
	"MoveTo := \"*** Move to: \" newPath NEWLINE\nHunk := \"@@\" [ header ] NEWLINE { HunkLine } [ \"*** End of File\" NEWLINE ]\n" +
	"HunkLine := (\" \" | \"-\" | \"+\") text NEWLINE\n\n" +
	"Example patch:\n\n" +
	"*** Begin Patch\n*** Add File: hello.txt\n+Hello world\n*** Update File: src/app.py\n*** Move to: src/main.py\n@@ def greet():\n-print(\"Hi\")\n+print(\"Hello, world!\")\n*** Delete File: obsolete.txt\n*** End Patch\n\n" +
	"It is important to remember:\n\n" +
	"- You must include a header with your intended action (Add/Delete/Update)\n" +
	"- You must prefix new lines with `+` even when creating a new file\n" +
	"- File references are relative to the working directory"

// Parameters is the function tool's schema: the patch in "input", as
// Codex's function form of the tool.
func Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"input": map[string]any{"type": "string", "description": "The entire contents of the apply_patch command"},
		},
		"required":             []any{"input"},
		"additionalProperties": false,
	}
}

// Args are the tool's arguments.
type Args struct {
	Input string `json:"input"`
}

// ErrNoInput is arguments without a patch.
var ErrNoInput = errors.New("the arguments must be a JSON object with the patch in \"input\"")

// ParseArgs reads the tool's arguments.
func ParseArgs(arguments string) (string, error) {
	var a Args
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

// FromHookInput turns a hook's updatedInput back into the tool's
// arguments: its "command" (or "input") is the new patch.
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
	data, err := json.Marshal(Args{Input: patch})

	return string(data), err //nolint:wrapcheck // a string always encodes
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
