package mcp

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/zalando/go-keyring"
	"golang.org/x/oauth2"
)

// Credentials are one server's OAuth login: the client uah registered (or
// the configured one), the token endpoint, and the tokens. They are stored
// as Codex stores them, under a key made of the server's name and URL, so
// a changed URL needs a new login.
type Credentials struct {
	ServerName   string   `json:"server_name"`
	ServerURL    string   `json:"server_url"`
	ClientID     string   `json:"client_id"`
	ClientSecret string   `json:"client_secret,omitempty"`
	TokenURL     string   `json:"token_url"`
	AuthStyle    int      `json:"auth_style,omitempty"`
	Scopes       []string `json:"scopes,omitempty"`
	AccessToken  string   `json:"access_token"`
	RefreshToken string   `json:"refresh_token,omitempty"`
	TokenType    string   `json:"token_type,omitempty"`
	// ExpiresAt is milliseconds since the epoch, as in Codex (0: unknown).
	ExpiresAt int64 `json:"expires_at,omitempty"`
}

// Token is the stored token for the oauth2 package.
func (c Credentials) Token() *oauth2.Token {
	t := &oauth2.Token{AccessToken: c.AccessToken, RefreshToken: c.RefreshToken, TokenType: c.TokenType}
	if c.ExpiresAt > 0 {
		t.Expiry = time.UnixMilli(c.ExpiresAt)
	}

	return t
}

// withToken returns the credentials with a new token; a refresh that
// returns no refresh token keeps the old one.
func (c Credentials) withToken(t *oauth2.Token) Credentials {
	c.AccessToken, c.TokenType = t.AccessToken, t.TokenType
	if t.RefreshToken != "" {
		c.RefreshToken = t.RefreshToken
	}
	c.ExpiresAt = 0
	if !t.Expiry.IsZero() {
		c.ExpiresAt = t.Expiry.UnixMilli()
	}

	return c
}

// CredentialStore keeps OAuth credentials by server. Load returns
// ErrNoCredentials when none are stored.
type CredentialStore interface {
	Load(server, url string) (Credentials, error)
	Save(c Credentials) error
	// Delete reports whether there was something to delete.
	Delete(server, url string) (bool, error)
}

// ErrNoCredentials is returned by a CredentialStore without a login for
// the server.
var ErrNoCredentials = errors.New("no stored OAuth credentials")

// Credential store modes: Codex's mcp_oauth_credentials_store values.
const (
	StoreAuto    = "auto"
	StoreFile    = "file"
	StoreKeyring = "keyring"
)

// StoreModes are the accepted mcp_oauth_credentials_store values.
var StoreModes = []string{StoreAuto, StoreFile, StoreKeyring}

// KeyringService names uah's entries in the OS keyring.
const KeyringService = "uah MCP Credentials"

// storeKey is Codex's store key (codex-rs/rmcp-client/src/oauth.rs): the
// server's name and the first 16 hex digits of a SHA-256 of its identity.
func storeKey(server, url string) string {
	identity, _ := json.Marshal(struct { //nolint:errchkjson // strings always encode
		Type    string            `json:"type"`
		URL     string            `json:"url"`
		Headers map[string]string `json:"headers"`
	}{"http", url, map[string]string{}})
	sum := sha256.Sum256(identity)

	return server + "|" + hex.EncodeToString(sum[:])[:16]
}

// NewCredentialStore returns the store for a mode: the OS keyring, a file
// (0600), or auto, which uses the keyring and falls back to the file when
// the keyring is unavailable or refuses the entry, as Codex does.
func NewCredentialStore(mode, file string) (CredentialStore, error) {
	switch mode {
	case StoreFile:
		return &FileStore{Path: file}, nil
	case StoreKeyring:
		return keyringStore{}, nil
	case "", StoreAuto:
		return autoStore{keyring: keyringStore{}, file: &FileStore{Path: file}}, nil
	}

	return nil, fmt.Errorf("unknown mcp_oauth_credentials_store %q (want one of %v)", mode, StoreModes)
}

// keyringStore keeps each server's credentials as one JSON entry.
type keyringStore struct{}

func (keyringStore) Load(server, url string) (Credentials, error) {
	data, err := keyring.Get(KeyringService, storeKey(server, url))
	if errors.Is(err, keyring.ErrNotFound) {
		return Credentials{}, ErrNoCredentials
	}
	if err != nil {
		return Credentials{}, fmt.Errorf("failed to read the keyring: %w", err)
	}
	var c Credentials
	if err := json.Unmarshal([]byte(data), &c); err != nil {
		return Credentials{}, fmt.Errorf("failed to decode the keyring entry: %w", err)
	}

	return c, nil
}

func (keyringStore) Save(c Credentials) error {
	data, err := json.Marshal(c)
	if err != nil {
		return fmt.Errorf("failed to encode credentials: %w", err)
	}
	if err := keyring.Set(KeyringService, storeKey(c.ServerName, c.ServerURL), string(data)); err != nil {
		return fmt.Errorf("failed to write the keyring: %w", err)
	}

	return nil
}

func (keyringStore) Delete(server, url string) (bool, error) {
	err := keyring.Delete(KeyringService, storeKey(server, url))
	if errors.Is(err, keyring.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to delete the keyring entry: %w", err)
	}

	return true, nil
}

// autoStore prefers the keyring. A save that the keyring refuses goes to
// the file; a save to the keyring removes the file's copy. Loads fall back
// to the file.
type autoStore struct {
	keyring CredentialStore
	file    CredentialStore
}

func (s autoStore) Load(server, url string) (Credentials, error) {
	c, err := s.keyring.Load(server, url)
	if err == nil {
		return c, nil
	}

	return s.file.Load(server, url) //nolint:wrapcheck // the file store's own error
}

func (s autoStore) Save(c Credentials) error {
	if err := s.keyring.Save(c); err != nil {
		return s.file.Save(c) //nolint:wrapcheck // the file store's own error
	}
	_, _ = s.file.Delete(c.ServerName, c.ServerURL)

	return nil
}

func (s autoStore) Delete(server, url string) (bool, error) {
	fromKeyring, kerr := s.keyring.Delete(server, url)
	fromFile, ferr := s.file.Delete(server, url)
	if ferr != nil {
		return fromKeyring, ferr
	}
	if kerr != nil && !fromFile {
		return false, kerr
	}

	return fromKeyring || fromFile, nil
}

// FileStore keeps every server's credentials in one JSON file, readable
// only by the user (0600), keyed as Codex's .credentials.json.
type FileStore struct {
	Path string

	mu sync.Mutex
}

func (s *FileStore) read() (map[string]Credentials, error) {
	data, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]Credentials{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", s.Path, err)
	}
	all := map[string]Credentials{}
	if err := json.Unmarshal(data, &all); err != nil {
		return nil, fmt.Errorf("failed to decode %s: %w", s.Path, err)
	}

	return all, nil
}

// write replaces the file atomically, so a crash never leaves half of it.
func (s *FileStore) write(all map[string]Credentials) error {
	data, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode credentials: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return fmt.Errorf("failed to create %s: %w", filepath.Dir(s.Path), err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.Path), ".credentials-*")
	if err != nil {
		return fmt.Errorf("failed to write credentials: %w", err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()

		return fmt.Errorf("failed to write credentials: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("failed to write credentials: %w", err)
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		return fmt.Errorf("failed to write credentials: %w", err)
	}
	if err := os.Rename(tmp.Name(), s.Path); err != nil {
		return fmt.Errorf("failed to write credentials: %w", err)
	}

	return nil
}

func (s *FileStore) Load(server, url string) (Credentials, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.read()
	if err != nil {
		return Credentials{}, err
	}
	c, ok := all[storeKey(server, url)]
	if !ok {
		return Credentials{}, ErrNoCredentials
	}

	return c, nil
}

func (s *FileStore) Save(c Credentials) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.read()
	if err != nil {
		return err
	}
	all[storeKey(c.ServerName, c.ServerURL)] = c

	return s.write(all)
}

func (s *FileStore) Delete(server, url string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.read()
	if err != nil {
		return false, err
	}
	key := storeKey(server, url)
	if _, ok := all[key]; !ok {
		return false, nil
	}
	delete(all, key)
	if len(all) == 0 {
		if err := os.Remove(s.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return false, fmt.Errorf("failed to remove %s: %w", s.Path, err)
		}

		return true, nil
	}

	return true, s.write(all)
}
