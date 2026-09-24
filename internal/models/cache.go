package models

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// DefaultTTL is how long a fetched list is fresh: Codex's
// DEFAULT_MODEL_CACHE_TTL (models-manager/src/manager.rs).
const DefaultTTL = 300 * time.Second

// cacheVersion changes when the entry's shape does; another version is a
// miss, as Codex treats another client_version.
const cacheVersion = 1

// entry is one provider's cached list: Codex's ModelsCacheEntry
// (models-manager/src/cache.rs) with the provider in the file name.
type entry struct {
	Version   int       `json:"version"`
	Provider  string    `json:"provider"`
	Identity  string    `json:"identity"`
	FetchedAt time.Time `json:"fetched_at"`
	ETag      string    `json:"etag,omitempty"`
	Models    []Model   `json:"models"`
}

func (e entry) fresh(now time.Time, ttl time.Duration) bool {
	return ttl > 0 && now.Sub(e.FetchedAt) <= ttl
}

// fileCache keeps one file per provider under dir: <dir>/<provider>.json.
type fileCache struct{ dir string }

func (c fileCache) path(provider string) string { return filepath.Join(c.dir, provider+".json") }

// load returns the provider's entry for identity at any age; ok is false
// for no file, another identity, or another version. A broken file is a miss.
func (c fileCache) load(provider, identity string) (entry, bool) {
	if c.dir == "" || identity == "" {
		return entry{}, false
	}
	data, err := os.ReadFile(c.path(provider))
	if err != nil {
		return entry{}, false
	}
	var e entry
	if json.Unmarshal(data, &e) != nil || e.Version != cacheVersion || e.Provider != provider || e.Identity != identity {
		return entry{}, false
	}

	return e, true
}

// store writes the entry atomically: a temporary file in the same
// directory, renamed over the old one.
func (c fileCache) store(e entry) error {
	if c.dir == "" {
		return nil
	}
	if err := os.MkdirAll(c.dir, 0o700); err != nil {
		return fmt.Errorf("failed to create the models cache directory: %w", err)
	}
	e.Version = cacheVersion
	data, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode the models cache: %w", err)
	}
	tmp, err := os.CreateTemp(c.dir, "."+e.Provider+"-*.json")
	if err != nil {
		return fmt.Errorf("failed to write the models cache: %w", err)
	}
	_, err = tmp.Write(data)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), c.path(e.Provider))
	}
	if err != nil {
		_ = os.Remove(tmp.Name())

		return fmt.Errorf("failed to write the models cache: %w", err)
	}

	return nil
}

// peek reads the provider's entry whatever its identity, for shell
// completion, which must not read credentials or call the network.
func (c fileCache) peek(provider string) (entry, bool) {
	if c.dir == "" {
		return entry{}, false
	}
	data, err := os.ReadFile(c.path(provider))
	if err != nil {
		return entry{}, false
	}
	var e entry
	if json.Unmarshal(data, &e) != nil || e.Version != cacheVersion || e.Provider != provider {
		return entry{}, false
	}

	return e, true
}
