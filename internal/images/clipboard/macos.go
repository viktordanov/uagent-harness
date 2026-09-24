package clipboard

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// MacOS reads the clipboard with osascript: a file copied in Finder first,
// as Codex prefers a file, then PNG data, then TIFF (what Preview copies).
type MacOS struct{ Exec Exec }

// The AppleScript for each clipboard class. A class the clipboard lacks
// fails, and the next is tried.
const (
	scriptFile = `POSIX path of (the clipboard as «class furl»)`
	scriptPNG  = `the clipboard as «class PNGf»`
	scriptTIFF = `the clipboard as «class TIFF»`
)

func (m MacOS) ReadImage(ctx context.Context) (Content, error) {
	if out, err := m.Exec(ctx, "osascript", "-e", scriptFile); err == nil {
		if path := strings.TrimSpace(string(out)); path != "" {
			return Content{Path: path}, nil
		}
	}
	for _, script := range []string{scriptPNG, scriptTIFF} {
		out, err := m.Exec(ctx, "osascript", "-e", script)
		if err != nil {
			if ctx.Err() != nil {
				return Content{}, fmt.Errorf("failed to read the clipboard: %w", ctx.Err())
			}

			continue
		}
		data, err := parseData(out)
		if err != nil {
			return Content{}, err
		}

		return Content{Data: data}, nil
	}

	return Content{}, ErrNoImage
}

// parseData decodes osascript's print of a data value, «data PNGf89504E…»:
// four characters of class, then the bytes in hex.
func parseData(out []byte) ([]byte, error) {
	s := bytes.TrimSpace(out)
	open, closing := []byte("«data "), []byte("»")
	if !bytes.HasPrefix(s, open) || !bytes.HasSuffix(s, closing) {
		return nil, errors.New("unexpected clipboard data from osascript")
	}
	body := s[len(open) : len(s)-len(closing)]
	if len(body) < 4 {
		return nil, errors.New("unexpected clipboard data from osascript")
	}
	data := make([]byte, hex.DecodedLen(len(body)-4))
	if _, err := hex.Decode(data, body[4:]); err != nil {
		return nil, fmt.Errorf("failed to decode the clipboard data: %w", err)
	}

	return data, nil
}
