package mcp

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
	gotoml "github.com/pelletier/go-toml/v2"
	"github.com/pelletier/go-toml/v2/unstable"
)

var bareKey = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// validName is Codex's server name rule for `codex mcp add`.
var validName = regexp.MustCompile(`^[A-Za-z0-9_\-:@/.]+$`)

// ValidName checks a server name as `codex mcp add` does.
func ValidName(name string) error {
	if !validName.MatchString(name) {
		return fmt.Errorf("invalid server name '%s' (use letters, numbers, '-', '_', ':', '@', '/', '.')", name)
	}

	return nil
}

// AddServer writes [mcp_servers.<name>] to a configuration file, replacing
// a server of that name, as `codex mcp add` does. The rest of the file,
// comments and formatting included, stays as it was: the server's tables
// are cut out and the new one is appended. The file is replaced
// atomically, and only when the result parses and the server validates.
func AddServer(path, name string, c ServerConfig) error {
	if err := ValidName(name); err != nil {
		return err
	}
	if err := c.Validate(); err != nil {
		return err
	}
	data, mode, err := readConfig(path)
	if err != nil {
		return err
	}
	data, _, err = cutServer(data, name)
	if err != nil {
		return err
	}
	body, err := encodeServer(c)
	if err != nil {
		return err
	}
	data = bytes.TrimRight(data, "\n")
	if len(data) > 0 {
		data = append(data, "\n\n"...)
	}
	data = append(data, "[mcp_servers."+tomlKey(name)+"]\n"...)
	data = append(data, body...)
	if err := checkServer(data, name, true); err != nil {
		return err
	}

	return writeConfig(path, data, mode)
}

// RemoveServer deletes [mcp_servers.<name>] and its subtables from a
// configuration file, leaving the rest as it was. ok is false when the
// file has no such server.
func RemoveServer(path, name string) (bool, error) {
	data, mode, err := readConfig(path)
	if err != nil {
		return false, err
	}
	out, cut, err := cutServer(data, name)
	if err != nil {
		return false, err
	}
	if err := checkServer(out, name, false); err != nil || !cut {
		return false, err
	}

	return true, writeConfig(path, out, mode)
}

func readConfig(path string) ([]byte, fs.FileMode, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, 0o600, nil
	}
	if err != nil {
		return nil, 0, fmt.Errorf("failed to read %s: %w", path, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to read %s: %w", path, err)
	}

	return data, info.Mode().Perm(), nil
}

// writeConfig replaces the file atomically with the same permissions.
func writeConfig(path string, data []byte, mode fs.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("failed to create %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".config-*.toml")
	if err != nil {
		return fmt.Errorf("failed to write %s: %w", path, err)
	}
	defer os.Remove(tmp.Name())
	_, err = tmp.Write(data)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(tmp.Name(), mode)
	}
	if err == nil {
		err = os.Rename(tmp.Name(), path)
	}
	if err != nil {
		return fmt.Errorf("failed to write %s: %w", path, err)
	}

	return nil
}

// span is a byte range of the file.
type span struct{ start, end int }

// cutServer removes the tables [mcp_servers.<name>] and
// [mcp_servers.<name>.*]. Each runs from its header line to the next
// header, less the blank and comment lines right before that header,
// which belong to what follows. cut reports whether there was one.
func cutServer(data []byte, name string) (out []byte, cut bool, err error) {
	var spans []span
	open := -1 // the start of the server table being cut, or -1
	p := unstable.Parser{}
	p.Reset(data)
	for p.NextExpression() {
		e := p.Expression()
		if e.Kind != unstable.Table && e.Kind != unstable.ArrayTable {
			continue
		}
		start, keys := headerStart(data, e)
		if open >= 0 {
			spans = append(spans, span{open, trimTail(data, start)})
			open = -1
		}
		if len(keys) >= 2 && keys[0] == "mcp_servers" && keys[1] == name {
			open = start
		}
	}
	if err := p.Error(); err != nil {
		return nil, false, fmt.Errorf("failed to parse the configuration: %w", err)
	}
	if open >= 0 {
		spans = append(spans, span{open, len(data)})
	}
	for _, sp := range slices.Backward(spans) {
		data = append(data[:sp.start:sp.start], data[sp.end:]...)
	}

	return data, len(spans) > 0, nil
}

// headerStart returns where a table header's line starts, and its keys.
func headerStart(data []byte, e *unstable.Node) (int, []string) {
	var keys []string
	first := -1
	for it := e.Key(); it.Next(); {
		k := it.Node()
		if first < 0 {
			first = int(k.Raw.Offset)
		}
		keys = append(keys, string(k.Data))
	}

	return bytes.LastIndexByte(data[:first], '\n') + 1, keys
}

// trimTail moves end back over the blank and comment lines before it,
// then forward again over the blank ones among them: the comments stay
// with the header they precede, and the section's trailing blank lines go
// with it.
func trimTail(data []byte, end int) int {
	blank := func(line string) bool { return strings.TrimSpace(line) == "" }
	for end > 0 {
		prev := bytes.LastIndexByte(data[:end-1], '\n') + 1
		if line := strings.TrimSpace(string(data[prev:end])); line != "" && !strings.HasPrefix(line, "#") {
			break
		}
		end = prev
	}
	for end < len(data) {
		next := bytes.IndexByte(data[end:], '\n')
		if next < 0 || !blank(string(data[end:end+next])) {
			break
		}
		end += next + 1
	}

	return end
}

// checkServer parses the edited file and checks that the server is there
// and valid (want), or gone. A server written some other way, such as an
// inline table, cannot be cut and fails here, leaving the file alone.
func checkServer(data []byte, name string, want bool) error {
	var file struct {
		MCPServers map[string]toml.Primitive `toml:"mcp_servers"`
	}
	meta, err := toml.Decode(string(data), &file)
	if err != nil {
		return fmt.Errorf("the edited configuration does not parse: %w", err)
	}
	prim, ok := file.MCPServers[name]
	if !want {
		if ok {
			return fmt.Errorf("mcp_servers.%s is not written as its own [mcp_servers.%s] table; edit the file by hand", name, name)
		}

		return nil
	}
	var c ServerConfig
	if !ok {
		return errors.New("the edited configuration lost the server")
	}
	if err := meta.PrimitiveDecode(prim, &c); err != nil {
		return fmt.Errorf("the edited configuration does not decode: %w", err)
	}

	return c.Validate()
}

// encodeServer writes the server's keys, sub-tables inline.
func encodeServer(c ServerConfig) ([]byte, error) {
	var buf bytes.Buffer
	enc := gotoml.NewEncoder(&buf).SetTablesInline(true).SetIndentTables(false)
	if err := enc.Encode(written(c)); err != nil {
		return nil, fmt.Errorf("failed to encode the server: %w", err)
	}

	return buf.Bytes(), nil
}

// writtenServer is what `uah mcp add` can set, in Codex's order.
type writtenServer struct {
	Command           string            `toml:"command,omitempty"`
	Args              []string          `toml:"args,omitempty"`
	Env               map[string]string `toml:"env,omitempty"`
	URL               string            `toml:"url,omitempty"`
	BearerTokenEnvVar string            `toml:"bearer_token_env_var,omitempty"`
	OAuthResource     string            `toml:"oauth_resource,omitempty"`
	OAuth             *writtenOAuth     `toml:"oauth,omitempty"`
}

type writtenOAuth struct {
	ClientID string `toml:"client_id"`
}

func written(c ServerConfig) writtenServer {
	w := writtenServer{
		Command: c.Command, Args: c.Args, Env: c.Env, URL: c.URL,
		BearerTokenEnvVar: c.BearerTokenEnvVar, OAuthResource: c.OAuthResource,
	}
	if c.OAuth != nil && c.OAuth.ClientID != "" {
		w.OAuth = &writtenOAuth{ClientID: c.OAuth.ClientID}
	}

	return w
}

// tomlKey quotes a name that is not a bare TOML key.
func tomlKey(name string) string {
	if bareKey.MatchString(name) {
		return name
	}

	return `"` + name + `"`
}
