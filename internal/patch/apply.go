// Adapted from openai/codex rust-v0.156.1 (Apache License 2.0, Copyright
// 2025 OpenAI): codex-rs/apply-patch/src/lib.rs (apply_hunks_to_files and
// print_summary).

package patch

import (
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Change is what one hunk does, computed against the files before it is
// written: an added file's content, a deleted file's old content, or an
// update's old and new content.
type Change struct {
	Op Op
	// Path and MovePath are as the patch names them; Abs and MoveAbs are
	// resolved against the working directory.
	Path, MovePath string
	Abs, MoveAbs   string
	Old, New       string
}

// Resolve makes a patch path absolute against cwd.
func Resolve(cwd, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}

	return filepath.Join(cwd, path)
}

// Paths are the absolute paths the hunks write: each file, and each move's
// destination.
func Paths(cwd string, hunks []Hunk) []string {
	var out []string
	for _, h := range hunks {
		out = append(out, Resolve(cwd, h.Path))
		if h.MovePath != "" {
			out = append(out, Resolve(cwd, h.MovePath))
		}
	}

	return out
}

// Compute works out every change without writing anything, reading the
// files as the earlier hunks of the same patch leave them. Its errors are
// Codex's messages.
func Compute(cwd string, hunks []Hunk) ([]Change, error) {
	if len(hunks) == 0 {
		return nil, errors.New("No files were modified.") //nolint:staticcheck // Codex's message
	}
	files := overlay{}
	out := make([]Change, 0, len(hunks))
	for _, h := range hunks {
		c := Change{Op: h.Op, Path: h.Path, Abs: Resolve(cwd, h.Path)}
		switch h.Op {
		case Add:
			c.New = h.Contents
			files.set(c.Abs, c.New)
		case Delete:
			old, err := files.read(c.Abs)
			if err != nil {
				return nil, fmt.Errorf("Failed to delete file %s: %w", c.Abs, err) //nolint:staticcheck // Codex's message
			}
			c.Old = old
			files.remove(c.Abs)
		case Update:
			old, err := files.read(c.Abs)
			if err != nil {
				return nil, fmt.Errorf("Failed to read file to update %s: %w", c.Abs, err) //nolint:staticcheck // Codex's message
			}
			c.New, err = updated(old, c.Abs, h.Chunks)
			if err != nil {
				return nil, err
			}
			c.Old = old
			if h.MovePath != "" {
				c.MovePath, c.MoveAbs = h.MovePath, Resolve(cwd, h.MovePath)
				files.remove(c.Abs)
				files.set(c.MoveAbs, c.New)
			} else {
				files.set(c.Abs, c.New)
			}
		}
		out = append(out, c)
	}

	return out, nil
}

// overlay is the files as the patch has changed them so far; a nil entry
// is a deleted file.
type overlay map[string]*string

func (o overlay) set(path, text string) { o[path] = &text }
func (o overlay) remove(path string)    { o[path] = nil }

func (o overlay) read(path string) (string, error) {
	if text, ok := o[path]; ok {
		if text == nil {
			return "", fs.ErrNotExist
		}

		return *text, nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", err //nolint:wrapcheck // the caller names the file
	}
	if info.IsDir() {
		return "", errors.New("it is a directory")
	}
	data, err := os.ReadFile(path)

	return string(data), err //nolint:wrapcheck // the caller names the file
}

// Write applies the changes to the files in order, creating missing
// parent directories. It stops at the first failure.
func Write(changes []Change) error {
	for _, c := range changes {
		if err := write(c); err != nil {
			return err
		}
	}

	return nil
}

func write(c Change) error {
	switch c.Op {
	case Add:
		return writeFile(c.Abs, c.New, 0o644)
	case Delete:
		if err := os.Remove(c.Abs); err != nil {
			return fmt.Errorf("Failed to delete file %s: %w", c.Abs, err) //nolint:staticcheck // Codex's message
		}
	case Update:
		perm := fs.FileMode(0o644)
		if info, err := os.Stat(c.Abs); err == nil {
			perm = info.Mode().Perm()
		}
		if c.MoveAbs == "" {
			return writeFile(c.Abs, c.New, perm)
		}
		if err := writeFile(c.MoveAbs, c.New, perm); err != nil {
			return err
		}
		if err := os.Remove(c.Abs); err != nil {
			return fmt.Errorf("Failed to remove original %s: %w", c.Abs, err) //nolint:staticcheck // Codex's message
		}
	}

	return nil
}

func writeFile(path, text string, perm fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("Failed to create parent directories for %s: %w", path, err) //nolint:staticcheck // Codex's message
	}
	if err := os.WriteFile(path, []byte(text), perm); err != nil {
		return fmt.Errorf("Failed to write file %s: %w", path, err) //nolint:staticcheck // Codex's message
	}

	return nil
}

// Summary is Codex's output for an applied patch: the files added, then
// modified (by their destination when moved), then deleted.
func Summary(changes []Change) string {
	var added, modified, deleted []string
	for _, c := range changes {
		switch c.Op {
		case Add:
			added = append(added, c.Path)
		case Update:
			modified = append(modified, cmp.Or(c.MovePath, c.Path))
		case Delete:
			deleted = append(deleted, c.Path)
		}
	}
	var b strings.Builder
	b.WriteString("Success. Updated the following files:\n")
	for _, group := range []struct {
		mark  string
		paths []string
	}{{"A", added}, {"M", modified}, {"D", deleted}} {
		for _, p := range group.paths {
			fmt.Fprintf(&b, "%s %s\n", group.mark, p)
		}
	}

	return b.String()
}
