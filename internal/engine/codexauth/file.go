package codexauth

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// file is one Codex auth file. Every Login that names the same path shares
// it, so the process refreshes a file once at a time: the sessions, their
// subagents, the usage reader, and the model list.
type file struct {
	path string

	// mu guards the cache: the file as last read, parsed again only when
	// its size, modification time, or inode changes.
	mu   sync.Mutex
	info os.FileInfo
	auth fileAuth
	err  error

	// refreshing holds the one refresh in flight. failed, under it, is the
	// refresh token the token endpoint refused for good, so it is not sent
	// again (Codex's permanent_refresh_failure, manager.rs).
	refreshing sync.Mutex
	failed     string
}

// files holds the files by absolute path.
var files sync.Map

func fileFor(path string) *file {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = filepath.Clean(path)
	}
	f, _ := files.LoadOrStore(abs, &file{path: abs})

	return f.(*file) //nolint:forcetypeassert // the map holds only files
}

// fileAuth is what uah reads from the auth file. raw is the whole file.
type fileAuth struct {
	token, accountID, refreshToken string
	lastRefresh                    time.Time
	raw                            []byte
}

// read returns the file's credentials, from the cache while the file is
// unchanged. A stat per request is cheap, and it picks up a refresh Codex
// wrote.
func (f *file) read() (fileAuth, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	info, err := os.Stat(f.path)
	if err == nil && f.info != nil && os.SameFile(info, f.info) &&
		info.ModTime().Equal(f.info.ModTime()) && info.Size() == f.info.Size() {
		return f.auth, f.err
	}
	f.auth, f.info, f.err = readAuthFile(f.path)

	return f.auth, f.err
}

// readAuthFile reads and parses the auth file, which must be private.
func readAuthFile(path string) (fileAuth, os.FileInfo, error) {
	file, err := os.Open(path)
	if err != nil {
		return fileAuth{}, nil, fmt.Errorf("failed to open the Codex auth file: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return fileAuth{}, nil, fmt.Errorf("failed to inspect the Codex auth file: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return fileAuth{}, info, errors.New("codex auth file must be a regular file with private permissions (chmod 600)")
	}
	const limit = 1 << 20
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return fileAuth{}, nil, fmt.Errorf("failed to read the Codex auth file: %w", err)
	}
	if len(data) > limit {
		return fileAuth{}, info, errors.New("invalid Codex auth file; it is larger than 1 MiB")
	}
	auth, err := parseAuth(data)

	return auth, info, err
}

// parseAuth reads the fields of Codex's AuthDotJson (storage.rs) that uah
// uses.
func parseAuth(data []byte) (fileAuth, error) {
	var auth struct {
		Mode   string `json:"auth_mode"`
		Tokens struct {
			AccessToken  string `json:"access_token"`
			AccountID    string `json:"account_id"`
			RefreshToken string `json:"refresh_token"`
		} `json:"tokens"`
		LastRefresh string `json:"last_refresh"`
	}
	if json.Unmarshal(data, &auth) != nil {
		return fileAuth{}, errors.New("invalid Codex auth file; expected a JSON object with tokens.access_token and tokens.account_id")
	}
	if auth.Mode != "" && auth.Mode != "chatgpt" {
		return fileAuth{}, errors.New("codex auth file is not a ChatGPT subscription login")
	}
	last, _ := time.Parse(time.RFC3339Nano, auth.LastRefresh)

	return fileAuth{
		token:        strings.TrimSpace(auth.Tokens.AccessToken),
		accountID:    strings.TrimSpace(auth.Tokens.AccountID),
		refreshToken: strings.TrimSpace(auth.Tokens.RefreshToken),
		lastRefresh:  last,
		raw:          data,
	}, nil
}

// lock takes uah's lock beside the auth file, so uah processes refresh one
// at a time: a refresh token works once. Codex takes no lock (it rewrites
// the file in place, storage.rs), so a refresh also reads the file again
// under the lock before it asks and before it writes.
func (f *file) lock(ctx context.Context) (func(), error) {
	name := filepath.Join(filepath.Dir(f.path), "."+filepath.Base(f.path)+".uah-lock")
	lf, err := os.OpenFile(name, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("failed to open the Codex auth lock: %w", err)
	}
	fd := int(lf.Fd()) // a descriptor fits an int
	for {
		err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return func() { _ = syscall.Flock(fd, syscall.LOCK_UN); _ = lf.Close() }, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			_ = lf.Close()

			return nil, fmt.Errorf("failed to lock the Codex auth file: %w", err)
		}
		select {
		case <-ctx.Done():
			_ = lf.Close()

			return nil, fmt.Errorf("failed to lock the Codex auth file: %w", ctx.Err())
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// write stores the refreshed tokens in the file whose contents are base, as
// Codex's persist_tokens does (manager.rs): the tokens the endpoint
// returned and last_refresh now, every other field kept in its place. It
// writes a private temporary file beside the file and renames it over the
// file, so no reader sees half of it.
func (f *file) write(base []byte, t tokens, now time.Time) (fileAuth, error) {
	data, err := updated(base, t, now)
	if err != nil {
		return fileAuth{}, err
	}
	target, err := filepath.EvalSymlinks(f.path)
	if err != nil {
		return fileAuth{}, fmt.Errorf("failed to find the Codex auth file: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), "."+filepath.Base(target)+".*.tmp")
	if err != nil {
		return fileAuth{}, fmt.Errorf("failed to write the Codex auth file: %w", err)
	}
	defer os.Remove(tmp.Name()) // after the rename, nothing is there
	err = errors.Join(tmp.Chmod(0o600), write(tmp, data), tmp.Sync(), tmp.Close())
	if err == nil {
		err = os.Rename(tmp.Name(), target)
	}
	if err != nil {
		return fileAuth{}, fmt.Errorf("failed to write the Codex auth file: %w", err)
	}

	return f.read()
}

func write(w io.Writer, data []byte) error {
	_, err := w.Write(data)

	return err // the caller wraps it
}

// updated is base with the refreshed tokens and last_refresh, indented as
// Codex writes it (serde_json's to_string_pretty).
func updated(base []byte, t tokens, now time.Time) ([]byte, error) {
	top, err := members(base)
	if err != nil {
		return nil, err
	}
	tokensValue := jsontext.Value(`{}`)
	if v, ok := get(top, "tokens"); ok {
		tokensValue = v
	}
	inner, err := members(tokensValue)
	if err != nil {
		return nil, err
	}
	for _, kv := range [][2]string{{"id_token", t.IDToken}, {"access_token", t.AccessToken}, {refreshToken, t.RefreshToken}} {
		if kv[1] != "" {
			inner = set(inner, kv[0], str(kv[1]))
		}
	}
	tokensValue, err = object(inner)
	if err != nil {
		return nil, err
	}
	top = set(top, "tokens", tokensValue)
	top = set(top, "last_refresh", str(now.UTC().Format(time.RFC3339Nano)))
	out, err := object(top)
	if err != nil {
		return nil, err
	}
	if err := out.Indent(jsontext.WithIndent("  "), jsontext.SpaceAfterColon(true)); err != nil {
		return nil, fmt.Errorf("failed to format the Codex auth file: %w", err)
	}

	return out, nil
}

// member is one member of a JSON object, in order.
type member struct {
	name  string
	value jsontext.Value
}

func members(obj []byte) ([]member, error) {
	invalid := errors.New("invalid Codex auth file; expected a JSON object")
	dec := jsontext.NewDecoder(bytes.NewReader(obj))
	if tok, err := dec.ReadToken(); err != nil || tok.Kind() != '{' {
		return nil, invalid
	}
	var out []member
	for dec.PeekKind() != '}' {
		tok, err := dec.ReadToken()
		if err != nil {
			return nil, invalid
		}
		name := tok.String() // before the next read voids the token
		value, err := dec.ReadValue()
		if err != nil {
			return nil, invalid
		}
		out = append(out, member{name: name, value: value.Clone()})
	}

	return out, nil
}

func get(ms []member, name string) (jsontext.Value, bool) {
	for _, m := range ms {
		if m.name == name {
			return m.value, true
		}
	}

	return nil, false
}

// set replaces the member's value in its place, or appends it.
func set(ms []member, name string, value jsontext.Value) []member {
	for i := range ms {
		if ms[i].name == name {
			ms[i].value = value

			return ms
		}
	}

	return append(ms, member{name: name, value: value})
}

func object(ms []member) (jsontext.Value, error) {
	var buf bytes.Buffer
	enc := jsontext.NewEncoder(&buf)
	err := enc.WriteToken(jsontext.BeginObject)
	for _, m := range ms {
		err = errors.Join(err, enc.WriteToken(jsontext.String(m.name)), enc.WriteValue(m.value))
	}
	if err = errors.Join(err, enc.WriteToken(jsontext.EndObject)); err != nil {
		return nil, fmt.Errorf("failed to encode the Codex auth file: %w", err)
	}

	return jsontext.Value(bytes.TrimSpace(buf.Bytes())), nil
}

func str(s string) jsontext.Value {
	v, _ := jsontext.AppendQuote(nil, s) // a Go string always quotes

	return v
}
