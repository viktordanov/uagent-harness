package mcp

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"

	"github.com/BurntSushi/toml"
	gotoml "github.com/pelletier/go-toml/v2"
	"github.com/viktordanov/uagent-harness/internal/config/tomledit"
)

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
	data, mode, err := tomledit.Read(path)
	if err != nil {
		return err
	}
	data, _, err = tomledit.CutTables(data, "mcp_servers", name)
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
	data = append(data, "[mcp_servers."+tomledit.Key(name)+"]\n"...)
	data = append(data, body...)
	if err := checkServer(data, name, true); err != nil {
		return err
	}

	return tomledit.Write(path, data, mode)
}

// RemoveServer deletes [mcp_servers.<name>] and its subtables from a
// configuration file, leaving the rest as it was. ok is false when the
// file has no such server.
func RemoveServer(path, name string) (bool, error) {
	data, mode, err := tomledit.Read(path)
	if err != nil {
		return false, err
	}
	out, cut, err := tomledit.CutTables(data, "mcp_servers", name)
	if err != nil {
		return false, err
	}
	if err := checkServer(out, name, false); err != nil || !cut {
		return false, err
	}

	return true, tomledit.Write(path, out, mode)
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
	// DefaultToolsApprovalMode is `uah mcp add --approve`.
	DefaultToolsApprovalMode ApprovalMode `toml:"default_tools_approval_mode,omitempty"`
}

type writtenOAuth struct {
	ClientID string `toml:"client_id"`
}

func written(c ServerConfig) writtenServer {
	w := writtenServer{
		Command: c.Command, Args: c.Args, Env: c.Env, URL: c.URL,
		BearerTokenEnvVar: c.BearerTokenEnvVar, OAuthResource: c.OAuthResource,
		DefaultToolsApprovalMode: c.DefaultToolsApprovalMode,
	}
	if c.OAuth != nil && c.OAuth.ClientID != "" {
		w.OAuth = &writtenOAuth{ClientID: c.OAuth.ClientID}
	}

	return w
}
