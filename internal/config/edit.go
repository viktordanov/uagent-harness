package config

import (
	"fmt"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/viktordanov/uagent-harness/internal/config/tomledit"
)

// SetValue sets key, dotted for a table ("tui.mouse"), in the configuration
// file at path, keeping its comments and formatting; a nil value removes
// the key. The edited file must still load: an unknown key or a wrong type
// leaves the file as it was. `uah mcp add` edits with the same editor.
func SetValue(path, key string, value any) error {
	data, mode, err := tomledit.Read(path)
	if err != nil {
		return err //nolint:wrapcheck // Read names the file
	}
	parts := strings.Split(key, ".")
	if value == nil {
		data, _, err = tomledit.Unset(data, parts)
	} else {
		data, err = tomledit.Set(data, parts, value)
	}
	if err != nil {
		return fmt.Errorf("failed to edit %s: %w", path, err)
	}
	var c Config
	if err := decodeBytes(path, data, &c); err != nil {
		return fmt.Errorf("the edit would break the file: %w", err)
	}

	return tomledit.Write(path, data, mode) //nolint:wrapcheck // Write names the file
}

// decodeBytes decodes a configuration file's contents; unknown keys are
// errors.
func decodeBytes(path string, data []byte, into *Config) error {
	meta, err := toml.Decode(string(data), into)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", path, err)
	}
	if undecoded := meta.Undecoded(); len(undecoded) > 0 {
		return fmt.Errorf("%s: unknown key %q", path, undecoded[0].String())
	}

	return nil
}
