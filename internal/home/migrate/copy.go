package migrate

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// copyTree copies the directory src into dst, creating dst. Files keep their
// permissions and modification times, so the index sees unchanged run
// records; symbolic links stay links, to the same absolute target. Session
// locks are left out, and so is anything that is not a file, a directory, or
// a link.
func copyTree(src, dst string) error {
	root, err := filepath.EvalSymlinks(src)
	if err != nil {
		return fmt.Errorf("failed to resolve %s: %w", src, err)
	}

	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		switch {
		case d.IsDir():
			return os.MkdirAll(target, 0o700)
		case d.Type()&fs.ModeSymlink != 0:
			return copyLink(path, target)
		case d.Type().IsRegular() && !strings.HasSuffix(d.Name(), ".lock"):
			return copyFile(path, target)
		}

		return nil
	})
}

// copyLink recreates the link at src as target, pointing where src does.
func copyLink(src, target string) error {
	dest, err := os.Readlink(src)
	if err != nil {
		return err
	}
	if !filepath.IsAbs(dest) {
		dest = filepath.Join(filepath.Dir(src), dest)
	}

	return os.Symlink(dest, target)
}

// copyFile copies one regular file with its mode and modification time.
func copyFile(src, target string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()

		return err
	}
	if err := out.Close(); err != nil {
		return err
	}

	return os.Chtimes(target, info.ModTime(), info.ModTime())
}
