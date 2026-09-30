package gitdiff

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/viktordanov/uah/internal/patch"
)

// Limits of what a diff shows.
const (
	// maxLines is how many diff lines one file keeps; the rest are counted.
	maxLines = 2000
	// maxUntrackedBytes is the largest untracked file shown line by line.
	maxUntrackedBytes = 512 << 10
	// maxUntracked is how many untracked files are read; the rest are
	// counted in Diff.MoreUntracked.
	maxUntracked = 200
	// sniffBytes is how much of a file is read to tell text from binary,
	// as git reads the first 8000 bytes for a NUL.
	sniffBytes = 8000
)

// Ops as patch.FileDiff names them.
const (
	opAdd    = "add"
	opUpdate = "update"
	opDelete = "delete"
)

// Notes on a file whose lines are not shown.
const (
	NoteBinary = "binary"
	NoteLarge  = "too large to show"
)

// File is one changed file for display: its diff, or a note (NoteBinary,
// NoteLarge, a submodule's line) in place of its lines.
type File struct {
	patch.FileDiff

	Note string
	// Untracked marks a file git does not track yet, shown as added.
	Untracked bool
}

// shown counts the lines the file keeps.
func (f *File) shown() int {
	n := 0
	for _, h := range f.Hunks {
		n += len(h.Lines)
	}

	return n
}

// Diff is a work tree's changes: the tracked files against HEAD (staged
// and unstaged together) and then the untracked files, which are not
// ignored, as additions.
type Diff struct {
	// Root is the top of the work tree; paths are relative to it.
	Root  string
	Files []File
	// MoreUntracked counts untracked files past the limit, not read.
	MoreUntracked int
}

// Added and Removed total the lines of every file.
func (d Diff) Added() int   { return d.sum(func(f File) int { return f.Added }) }
func (d Diff) Removed() int { return d.sum(func(f File) int { return f.Removed }) }

func (d Diff) sum(n func(File) int) int {
	total := 0
	for _, f := range d.Files {
		total += n(f)
	}

	return total
}

// Collect reads the changes of the work tree that holds dir, or returns
// ErrNotRepo. A clean tree has no files.
func Collect(ctx context.Context, dir string) (Diff, error) {
	root, err := Root(ctx, dir)
	if err != nil {
		return Diff{}, err
	}
	base := "HEAD"
	if !hasHead(ctx, root) {
		if base, err = emptyTree(ctx, root); err != nil {
			return Diff{}, err
		}
	}
	out, err := git(ctx, root, "diff", "--no-color", "--no-ext-diff", "--no-textconv", "--submodule=short", "--ignore-submodules=dirty", "-M", base, "--")
	if err != nil {
		return Diff{}, err
	}
	d := Diff{Root: root, Files: parse(out)}
	list, err := git(ctx, root, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return Diff{}, err
	}
	read := 0
	for p := range strings.SplitSeq(list, "\x00") {
		switch {
		case p == "":
		case read >= maxUntracked:
			d.MoreUntracked++
		default:
			d.Files, read = append(d.Files, untracked(root, p)), read+1
		}
	}

	return d, nil
}

// untracked reads an untracked file as an added one: its lines, or a note
// when it is binary or too large.
func untracked(root, rel string) File {
	f := File{Op: opAdd, Path: rel, Untracked: true}
	path := filepath.Join(root, filepath.FromSlash(rel))
	info, err := os.Lstat(path)
	switch {
	case err != nil:
		f.Note = err.Error()

		return f
	case info.Mode()&os.ModeSymlink != 0:
		target, _ := os.Readlink(path)
		f.Note = "symbolic link to " + target

		return f
	case !info.Mode().IsRegular():
		f.Note = "not a regular file"

		return f
	case info.Size() > maxUntrackedBytes:
		f.Note = fmt.Sprintf("%s (%d KB)", NoteLarge, info.Size()>>10)

		return f
	}
	data, err := os.ReadFile(path)
	if err != nil {
		f.Note = err.Error()

		return f
	}
	if bytes.IndexByte(data[:min(len(data), sniffBytes)], 0) >= 0 {
		f.Note = NoteBinary

		return f
	}
	addLines(&f, data)

	return f
}

// addLines puts every line of an added file into one hunk.
func addLines(f *File, data []byte) {
	if len(data) == 0 {
		return
	}
	text := strings.TrimSuffix(string(data), "\n")
	h := patch.DiffHunk{}
	for i, line := range strings.Split(text, "\n") {
		f.Added++
		if len(h.Lines) >= maxLines {
			f.Omitted++

			continue
		}
		h.Lines = append(h.Lines, patch.DiffLine{Kind: "+", New: i + 1, Text: strings.TrimSuffix(line, "\r")})
	}
	f.Hunks = []patch.DiffHunk{h}
}
