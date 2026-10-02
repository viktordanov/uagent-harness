package embedded

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"sync"
	"sync/atomic"
	"time"
	"uuid"

	"github.com/viktordanov/uah-core/harness/inbox"
	"github.com/viktordanov/uah-core/harness/llm"
	"github.com/viktordanov/uah-core/harness/operation"
	"github.com/viktordanov/uah-core/harness/session"
	"github.com/viktordanov/uah-core/harness/sessionstore"
	"github.com/viktordanov/uah-core/harness/sessionstore/localfile"
)

var (
	// syncWindow is how long a record that does not need a sync waits for
	// one.
	syncWindow = 100 * time.Millisecond
	// sessionSyncs counts the session files' syncs, for the perf harness.
	sessionSyncs atomic.Int64
	// onSync, when set, gets the size of the file each sync made durable.
	onSync func(path string, size int64)

	errLogClosed = errors.New("the session file is closed")
)

// SessionSyncs is how many times the engine has synced a session file.
func SessionSyncs() int64 { return sessionSyncs.Load() }

// logStore is the run's session store with uah's own write path: it
// appends the lines localfile's Append methods and SaveOperation write,
// byte for byte, but where localfile syncs the file after each, it syncs
// only before a record a crash must not lose is reported written, and
// covers every record before it with that sync (group commit). Reads go
// to localfile, which reads the file, so they see every record written.
// See docs/design/state.md.
//
// A record is written at once, so a crash of the process loses nothing;
// one of the system loses the records after the last sync. The file is
// synced before the call returns after:
//   - an operation state that is not terminal: the write-ahead point of
//     the operation's next step, or a command's process group and exit
//     code, which a resume and the harness read;
//   - a tool status that ends an operation other than a command, whose
//     result a resume from the state before would not have (a command's
//     exit code is in read_out);
//   - a model response with no tool call, which ends the turn.
//
// Any other record is synced with the next of those, within syncWindow,
// or at Close. A caller that needs a sync while another runs waits for
// it, and the next one covers both.
//
// It refuses what localfile would refuse on the way to a line its reader
// could not apply: a turn out of order, a response or status for a turn
// not the session's own, a state for an unknown operation.
//
// Its one read of the file, at the first write or read, builds that check
// and serves the usage seed's and the coordinator's restore's pages, where
// localfile decodes the whole file per page and again for its first
// append. The items it writes join them, until it has served a page: the
// next item it writes sends later reads to the file.
type logStore struct {
	*localfile.Store

	path string
	id   session.ID
	now  func() time.Time

	mu        sync.Mutex
	f         *os.File
	size      int64 // the bytes written
	head      logHead
	observers map[sessionstore.ObserverID]sessionstore.Observer
	order     []sessionstore.ObserverID
	timer     *time.Timer
	closed    bool
	items     []sessionstore.Item // the history, while pages come from it
	served    bool                // a page came from items
	err       error               // a write or sync failed: the file's end is unknown

	syncMu sync.Mutex
	synced int64
}

func newLogStore(store *localfile.Store, path string, id session.ID) *logStore {
	return &logStore{Store: store, path: path, id: id, now: time.Now, observers: map[sessionstore.ObserverID]sessionstore.Observer{}}
}

func (l *logStore) AddObserver(o sessionstore.Observer) sessionstore.ObserverID {
	id := uuid.New()
	l.observers[id], l.order = o, append(l.order, id)

	return id
}

func (l *logStore) RemoveObserver(id sessionstore.ObserverID) {
	delete(l.observers, id)
	l.order = slices.DeleteFunc(l.order, func(o sessionstore.ObserverID) bool { return o == id })
}

func (l *logStore) AppendInput(ctx context.Context, id session.ID, in inbox.Input) error {
	return l.append(ctx, id, sessionstore.ItemInput, in, nil, false, func(*logHead) error { return in.Validate() }) // localfile's message
}

func (l *logStore) AppendTurn(ctx context.Context, id session.ID, t session.Turn) error {
	return l.append(ctx, id, sessionstore.ItemTurn, t, nil, false, func(h *logHead) error {
		if _, exists := h.turns[t.ID]; t.ID == "" || exists || t.PreviousTurnID != h.last {
			return fmt.Errorf("append turn %q after %q to session %q, whose last turn is %q: %w", t.ID, t.PreviousTurnID, id, h.last, fs.ErrInvalid)
		}
		h.turns[t.ID], h.last = true, t.ID

		return nil
	})
}

func (l *logStore) AppendModelResponse(ctx context.Context, id session.ID, r sessionstore.ModelResponse) error {
	endsTurn := !slices.ContainsFunc(r.Response.Output, func(o llm.Item) bool { return o.Type == llm.ItemToolCall })

	return l.append(ctx, id, sessionstore.ItemModelResponse, r, nil, endsTurn, func(h *logHead) error {
		if !h.turns[r.TurnID] || h.responded[r.TurnID] {
			return fmt.Errorf("append model response for turn %q: %w", r.TurnID, fs.ErrInvalid)
		}
		h.responded[r.TurnID] = true

		return nil
	})
}

func (l *logStore) AppendToolCallStatus(ctx context.Context, id session.ID, s sessionstore.ToolCallStatus) error {
	ops := s.Operations
	s.Operations = nil
	ends := slices.ContainsFunc(ops, func(op operation.Operation) bool { return op.Type != operation.TypeShell && finalOperation(op.Status) })

	return l.append(ctx, id, sessionstore.ItemToolCallStatus, s, ops, ends, func(h *logHead) error {
		key := [2]string{string(s.TurnID), s.CallID}
		if !h.turns[s.TurnID] || s.CallID == "" {
			return fmt.Errorf("append tool-call status for turn %q and call %q: %w", s.TurnID, s.CallID, fs.ErrInvalid)
		}
		if h.statuses[key] {
			return nil
		}
		for _, op := range ops {
			if _, exists := h.ops[op.ID]; exists {
				return fmt.Errorf("initialize operation %q: %w", op.ID, fs.ErrExist)
			}
		}
		h.init(key, ops)

		return nil
	})
}

func (l *logStore) SaveOperation(ctx context.Context, id session.ID, v operation.Operation) error {
	return l.write(ctx, id, func(h *logHead) ([]byte, error) {
		if known, ok := h.ops[v.ID]; !ok || known.Type != v.Type || known.Version != v.Version {
			return nil, fmt.Errorf("save operation %q in session %q: unknown, or its type or version changed: %w", v.ID, id, fs.ErrInvalid)
		}

		return logLine("operation", struct{ Operation operation.Operation }{v})
	}, !finalOperation(v.Status))
}

// append writes the item; check refuses it or records it in the head,
// once the item is encoded.
func (l *logStore) append(ctx context.Context, id session.ID, kind sessionstore.ItemKind, data any, ops []operation.Operation, durable bool, check func(*logHead) error) error {
	var item sessionstore.Item
	err := l.write(ctx, id, func(h *logHead) ([]byte, error) {
		item = sessionstore.Item{Sequence: h.seq + 1, RecordedAt: l.now().UTC(), Kind: kind, Data: data}
		line, err := logLine("item", itemRecord{Item: item, Operations: ops})
		if err == nil {
			err = check(h)
		}
		if err != nil {
			return nil, err
		}
		h.seq++
		if s, ok := data.(sessionstore.ToolCallStatus); ok {
			s.Operations = ops
			item.Data = s
		}
		if l.served {
			l.items = nil
		} else if l.items != nil {
			l.items = append(l.items, item)
		}

		return line, nil
	}, durable)
	if err != nil {
		return err
	}
	for _, o := range l.order {
		l.observers[o](id, item)
	}

	return nil
}

// write appends the line record makes, and syncs the file when durable is
// set or schedules a sync.
func (l *logStore) write(ctx context.Context, id session.ID, record func(*logHead) ([]byte, error), durable bool) error {
	if err := context.Cause(ctx); err != nil {
		return err // as localfile returns it
	}
	l.mu.Lock()
	err := l.open(ctx, id)
	var line []byte
	if err == nil {
		line, err = record(&l.head)
	}
	if err == nil {
		var n int
		n, err = l.f.Write(line)
		l.size += int64(n)
		if err != nil {
			l.err = fmt.Errorf("failed to append to the session file: %w", err)
		}
	}
	if err == nil && !durable && l.timer == nil {
		l.timer = time.AfterFunc(syncWindow, func() { _ = l.sync() })
	}
	l.mu.Unlock()
	if err == nil && durable {
		err = l.sync()
	}

	return err
}

// open opens the file for the first write, after the head of its records
// and a record a crash left incomplete, which localfile's reader ignores.
func (l *logStore) open(ctx context.Context, id session.ID) error {
	switch {
	case id != l.id:
		return fmt.Errorf("session %q is not the run's %q: %w", id, l.id, fs.ErrInvalid)
	case l.err != nil:
		return l.err
	case l.closed:
		return errLogClosed
	case l.f != nil:
		return nil
	}
	items, err := allItems(ctx, l.Store, id)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(l.path, os.O_RDWR|os.O_APPEND, 0)
	if err != nil {
		return fmt.Errorf("failed to open the session file: %w", err)
	}
	size, err := committedSize(f)
	if err == nil {
		err = f.Truncate(size)
	}
	if err != nil {
		_ = f.Close()

		return fmt.Errorf("failed to open the session file: %w", err)
	}
	l.f, l.size, l.head, l.items = f, size, newLogHead(items), items
	l.syncMu.Lock()
	l.synced = size
	l.syncMu.Unlock()

	return nil
}

// Items serves the run's session from the items read at the start, until
// they are stale.
func (l *logStore) Items(ctx context.Context, id session.ID, after sessionstore.Sequence, limit int) (sessionstore.Page, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if id == l.id && limit > 0 && !l.served && !l.closed {
		if err := l.open(ctx, id); err != nil {
			return sessionstore.Page{}, err
		}
	}
	if id != l.id || limit <= 0 || l.items == nil {
		return l.Store.Items(ctx, id, after, limit) // localfile's errors pass through
	}
	l.served = true
	rest := l.items[min(after, sessionstore.Sequence(len(l.items))):] // item i has sequence i+1
	n := min(limit, len(rest))

	return sessionstore.Page{Items: append([]sessionstore.Item(nil), rest[:n]...), NextAfter: after + sessionstore.Sequence(n), More: n < len(rest)}, nil
}

// committedSize is the size of the file up to its last line end.
func committedSize(f *os.File) (int64, error) {
	st, err := f.Stat()
	if err != nil {
		return 0, err // the caller wraps it
	}
	buf := make([]byte, 4096)
	for end := st.Size(); end > 0; {
		start := max(0, end-int64(len(buf)))
		n, err := f.ReadAt(buf[:end-start], start)
		if err != nil {
			return 0, err
		}
		if i := bytes.LastIndexByte(buf[:n], '\n'); i >= 0 {
			return start + int64(i) + 1, nil
		}
		end = start
	}

	return 0, nil
}

// sync makes every record written so far durable, unless a sync that
// started after the last of them has done so.
func (l *logStore) sync() error {
	l.mu.Lock()
	want, f, err := l.size, l.f, l.err
	l.mu.Unlock()
	if err != nil || f == nil {
		return err
	}
	l.syncMu.Lock()
	defer l.syncMu.Unlock()
	if l.synced >= want {
		return nil
	}
	l.mu.Lock()
	upto := l.size
	if l.timer != nil {
		l.timer.Stop()
		l.timer = nil
	}
	l.mu.Unlock()
	if err := f.Sync(); err != nil {
		l.mu.Lock()
		l.err = fmt.Errorf("failed to sync the session file: %w", err)
		l.mu.Unlock()

		return l.err
	}
	l.synced = upto
	sessionSyncs.Add(1)
	if onSync != nil {
		onSync(l.path, upto)
	}

	return nil
}

// Close syncs the file and closes it.
func (l *logStore) Close() error {
	err := l.sync()
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.timer != nil {
		l.timer.Stop()
		l.timer = nil
	}
	if l.f != nil {
		err = errors.Join(err, l.f.Close())
	}
	l.f, l.closed = nil, true

	return err
}

// logHead is what the writer checks a record against: the items'
// sequence, the turns (true for the session's own, after its last fork),
// and the session's own responses, statuses, and operations.
type logHead struct {
	seq       sessionstore.Sequence
	last      session.TurnID
	turns     map[session.TurnID]bool
	responded map[session.TurnID]bool
	statuses  map[[2]string]bool
	ops       map[operation.ID]operation.Operation
}

func newLogHead(items []sessionstore.Item) logHead {
	h := logHead{turns: map[session.TurnID]bool{}}
	h.reset()
	for _, it := range items {
		h.seq = it.Sequence
		switch d := it.Data.(type) {
		case session.Turn:
			h.turns[d.ID], h.last = true, d.ID
		case sessionstore.ModelResponse:
			h.responded[d.TurnID] = true
		case sessionstore.ToolCallStatus:
			if key := [2]string{string(d.TurnID), d.CallID}; !h.statuses[key] {
				h.init(key, d.Operations)
			}
		case sessionstore.Fork:
			h.reset()
		}
	}

	return h
}

// reset leaves the turns so far to the parent, as a fork does.
func (h *logHead) reset() {
	for id := range h.turns {
		h.turns[id] = false
	}
	h.responded, h.statuses, h.ops = map[session.TurnID]bool{}, map[[2]string]bool{}, map[operation.ID]operation.Operation{}
}

// init records the first status of a call and the operations it starts.
func (h *logHead) init(key [2]string, ops []operation.Operation) {
	h.statuses[key] = true
	for _, op := range ops {
		h.ops[op.ID] = operation.Operation{Type: op.Type, Version: op.Version}
	}
}

// itemRecord is localfile's item line: a tool-call status's operations go
// beside the item.
type itemRecord struct {
	Item       sessionstore.Item
	Operations []operation.Operation `json:",omitempty"`
}
