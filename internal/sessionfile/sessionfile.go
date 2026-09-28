// Package sessionfile reads the runner's session file,
// sessions/<id>.session.jsonl, by the rules its README documents, with no
// code from the runner: a header, then items numbered by Sequence, the
// cursor a reader pages with. The runner writes the file and uah never
// changes it; the tests read recorded files through this package, so a
// change in the runner's format fails here first.
package sessionfile

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"time"
)

// Version is the format version this package reads (the header's Version).
const Version = 2

// Kind is what an item records.
type Kind string

// The item kinds. A reader skips a kind it does not show.
const (
	KindFork           Kind = "fork"
	KindInput          Kind = "input"
	KindTurn           Kind = "turn"
	KindModelResponse  Kind = "model_response"
	KindToolCallStatus Kind = "tool_call_status"
)

// ErrVersion means the file has a format version this package does not read.
var ErrVersion = errors.New("unsupported session file version")

// Header is the file's first line.
type Header struct {
	Version   int
	ID        string
	CreatedAt time.Time
}

// Item is one item record. Data decodes by Kind (Decode); Operations are the
// operation snapshots a tool_call_status item starts, as raw JSON.
type Item struct {
	Sequence   uint64
	RecordedAt time.Time
	Kind       Kind
	Data       json.RawMessage
	Operations []json.RawMessage
}

// Decode decodes the item's data into v, such as an Input for an input item.
func (it Item) Decode(v any) error {
	if err := json.Unmarshal(it.Data, v); err != nil {
		return fmt.Errorf("failed to decode %s item %d: %w", it.Kind, it.Sequence, err)
	}

	return nil
}

// Page is items after a cursor, as the runner's own Items call pages them:
// Next is the last item's Sequence (the cursor itself when there are none)
// and More says that items follow.
type Page struct {
	Items []Item
	Next  uint64
	More  bool
}

// record is one line: {"type": ..., "data": ...}.
type record struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

type headerData struct {
	Version int
	Session struct {
		ID        string
		CreatedAt time.Time
	}
}

type itemData struct {
	Item       Item
	Operations []json.RawMessage
}

// Read reads the header and the items with a Sequence after the cursor, at
// most limit of them (all when limit is 0). BeforeFirst (0) starts at the
// first item. A last line without its newline is a write in progress and is
// not read.
func Read(path string, after uint64, limit int) (Header, Page, error) {
	f, err := os.Open(path)
	if err != nil {
		return Header{}, Page{}, fmt.Errorf("failed to open the session file: %w", err)
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 1<<16)
	var h Header
	page := Page{Next: after}
	for n := 0; ; n++ {
		line, err := r.ReadBytes('\n')
		if errors.Is(err, io.EOF) {
			return h, page, nil // an unterminated line is not committed
		}
		if err != nil {
			return Header{}, Page{}, fmt.Errorf("failed to read the session file: %w", err)
		}
		if n == 0 {
			if h, err = header(line); err != nil {
				return Header{}, Page{}, err
			}

			continue
		}
		it, ok, err := item(line)
		if err != nil {
			return Header{}, Page{}, fmt.Errorf("line %d: %w", n+1, err)
		}
		if !ok || it.Sequence <= after {
			continue
		}
		if limit > 0 && len(page.Items) == limit {
			page.More = true

			return h, page, nil
		}
		page.Items = append(page.Items, it)
		page.Next = it.Sequence
	}
}

// BeforeFirst is the cursor before the first item.
const BeforeFirst uint64 = 0

// header decodes the first line and checks its version.
func header(line []byte) (Header, error) {
	var rec record
	if err := json.Unmarshal(line, &rec); err != nil || rec.Type != "session" {
		return Header{}, errors.New("the session file does not start with a session record")
	}
	var d headerData
	if err := json.Unmarshal(rec.Data, &d); err != nil {
		return Header{}, fmt.Errorf("failed to decode the session record: %w", err)
	}
	if d.Version != Version {
		return Header{}, fmt.Errorf("%w %d (want %d)", ErrVersion, d.Version, Version)
	}

	return Header{Version: d.Version, ID: d.Session.ID, CreatedAt: d.Session.CreatedAt}, nil
}

// item decodes an item line; ok is false for other records (operation
// updates), which a reader of the history skips.
func item(line []byte) (Item, bool, error) {
	var rec record
	if err := json.Unmarshal(line, &rec); err != nil {
		return Item{}, false, fmt.Errorf("failed to decode a record: %w", err)
	}
	if rec.Type != "item" {
		return Item{}, false, nil
	}
	var d itemData
	if err := json.Unmarshal(rec.Data, &d); err != nil {
		return Item{}, false, fmt.Errorf("failed to decode an item: %w", err)
	}
	d.Item.Operations = d.Operations

	return d.Item, true, nil
}

// Last returns the file's last item, reading from the end, so it costs the
// same for any length of history. found is false when the file has no item
// yet.
func Last(path string) (it Item, found bool, err error) {
	f, err := os.Open(path)
	if err != nil {
		return Item{}, false, fmt.Errorf("failed to open the session file: %w", err)
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return Item{}, false, fmt.Errorf("failed to read the session file: %w", err)
	}
	size := st.Size()
	for window := min(int64(1<<16), size); ; window = min(window*4, size) {
		buf := make([]byte, window)
		if _, err := f.ReadAt(buf, size-window); err != nil && !errors.Is(err, io.EOF) {
			return Item{}, false, fmt.Errorf("failed to read the session file: %w", err)
		}
		it, found, err := lastItem(buf, window == size)
		if err != nil || found || window == size {
			return it, found, err
		}
	}
}

// lastItem finds the last committed item line in the tail of a file; whole
// says the tail is the whole file, so its first line is complete.
func lastItem(tail []byte, whole bool) (Item, bool, error) {
	committed, _, ok := bytes.CutLast(tail, []byte{'\n'})
	if !ok {
		return Item{}, false, nil
	}
	lines := bytes.Split(committed, []byte{'\n'})
	for i, line := range slices.Backward(lines) {
		if i == 0 && !whole {
			break // may start mid-line; a larger window reads it
		}
		if !bytes.Contains(line, []byte(`"type":"item"`)) {
			continue
		}
		it, ok, err := item(line)
		if err != nil || ok {
			return it, ok, err
		}
	}

	return Item{}, false, nil
}
