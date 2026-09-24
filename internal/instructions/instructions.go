// Package instructions finds instruction files (AGENTS.md, as Codex does) and
// builds the host prompt for the runner. The runner reads no
// instruction files itself, and setting its system prompt replaces its own
// host prompt, so the runner's default text is kept in front.
package instructions

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// DefaultMaxBytes caps the assembled instructions, as Codex does.
const DefaultMaxBytes = 32 * 1024

// RunnerHostPrompt is unreal-agent-runner v0.1.1's default host prompt
// (cmd/internal/agentrunner/run.go), kept verbatim.
const RunnerHostPrompt = `You are an AI agent running inside an isolated sandbox container.

## Guidelines
- Save output files to the workspace root.
- For large datasets, inspect a sample first before processing everything.
`

// File is one instruction file.
type File struct {
	Path  string
	Bytes int
}

// Options are Codex's AGENTS.md settings (codex-rs/core/src/agents_md.rs).
type Options struct {
	// FallbackFilenames are tried after AGENTS.override.md and AGENTS.md in
	// each directory, in order; Codex's project_doc_fallback_filenames,
	// empty by default.
	FallbackFilenames []string
	// RootMarkers find the project root by walking up from the workspace;
	// Codex's project_root_markers. Nil means [".git"]; an empty list
	// disables walking up.
	RootMarkers []string
}

// Discover lists instruction files in the order they apply: the user file
// first, then one file per directory from the project root down to the
// workspace. In each directory it takes AGENTS.override.md, else AGENTS.md,
// else the first fallback that exists. userFiles are candidates for the user
// file, first match wins.
func Discover(workspace string, userFiles []string, opts Options) ([]File, error) {
	var files []File
	for _, p := range userFiles {
		if f, ok, err := stat(p); err != nil {
			return nil, err
		} else if ok {
			files = append(files, f)

			break
		}
	}
	dirs, err := ProjectDirs(workspace, opts.RootMarkers)
	if err != nil {
		return nil, err
	}
	names := append([]string{"AGENTS.override.md", "AGENTS.md"}, opts.FallbackFilenames...)
	for _, dir := range dirs {
		for _, name := range names {
			if name == "" || strings.ContainsAny(name, `/\`) {
				continue // Codex ignores fallbacks that are paths
			}
			f, ok, err := stat(filepath.Join(dir, name))
			if err != nil {
				return nil, err
			}
			if ok {
				files = append(files, f)

				break
			}
		}
	}

	return files, nil
}

// ProjectDirs returns the directories from the project root (the nearest
// ancestor holding one of markers; nil means .git) down to the workspace, or
// just the workspace when no ancestor has a marker or markers is empty.
func ProjectDirs(workspace string, markers []string) ([]string, error) {
	abs, err := filepath.Abs(workspace)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve workspace: %w", err)
	}
	if markers == nil {
		markers = []string{".git"}
	}
	if len(markers) == 0 {
		return []string{abs}, nil
	}
	var chain []string
	for dir := abs; ; dir = filepath.Dir(dir) {
		chain = append(chain, dir)
		if hasMarker(dir, markers) {
			break
		}
		if filepath.Dir(dir) == dir {
			return []string{abs}, nil
		}
	}
	// chain runs workspace -> root; reverse it to root -> workspace.
	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}

	return chain, nil
}

func hasMarker(dir string, markers []string) bool {
	for _, m := range markers {
		if _, err := os.Stat(filepath.Join(dir, m)); err == nil {
			return true
		}
	}

	return false
}

func stat(path string) (File, bool, error) {
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return File{}, false, nil
	}
	if err != nil {
		return File{}, false, fmt.Errorf("failed to read %s: %w", path, err)
	}
	if !info.Mode().IsRegular() || info.Size() == 0 {
		return File{}, false, nil
	}

	return File{Path: path, Bytes: int(info.Size())}, true, nil
}

// Assemble joins the files in order under headers naming each path. It stops
// before the file that would pass maxBytes; a first file larger than the cap
// is cut. used lists the files included.
func Assemble(files []File, maxBytes int) (text string, used []File, truncated bool, err error) {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}
	var b strings.Builder
	for _, f := range files {
		data, err := os.ReadFile(f.Path)
		if err != nil {
			return "", nil, false, fmt.Errorf("failed to read %s: %w", f.Path, err)
		}
		if strings.TrimSpace(string(data)) == "" {
			continue // Codex skips blank files
		}
		block := fmt.Sprintf("## %s\n\n%s\n", f.Path, strings.TrimSpace(string(data)))
		if b.Len()+len(block) > maxBytes {
			truncated = true
			if b.Len() == 0 {
				cut := block[:maxBytes]
				for !utf8.ValidString(cut) {
					cut = cut[:len(cut)-1]
				}
				b.WriteString(cut)
				used = append(used, f)
			}

			break
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(block)
		used = append(used, f)
	}

	return b.String(), used, truncated, nil
}

// HostPrompt builds the runner's system prompt: its default host prompt
// followed by the instructions. With no instructions it returns "", which
// leaves the runner's own prompt untouched.
func HostPrompt(instructions string) string {
	if strings.TrimSpace(instructions) == "" {
		return ""
	}

	return RunnerHostPrompt + "\n# Project instructions\n\nFollow these instructions from the project and the user. Later files are more specific and take precedence.\n\n" + instructions
}
