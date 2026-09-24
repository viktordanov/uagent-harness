package mcp

import (
	"fmt"
	"io"
	"maps"
	"net/http"
	"os/exec"
	"slices"
	"strings"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// defaultEnv is what a stdio server gets from uah's environment before
// env_vars and env, as in Codex (codex-rs/rmcp-client/src/utils.rs).
var defaultEnv = []string{"HOME", "LOGNAME", "PATH", "SHELL", "USER", "__CF_USER_TEXT_ENCODING", "LANG", "LC_ALL", "TERM", "TMPDIR", "TZ"}

// transport builds the SDK transport for a server.
func (m *Manager) transport(c ServerConfig) (sdk.Transport, error) {
	if c.URL != "" {
		headers, err := m.headers(c)
		if err != nil {
			return nil, err
		}

		return &sdk.StreamableClientTransport{
			Endpoint:   c.URL,
			HTTPClient: &http.Client{Transport: headerTransport{base: http.DefaultTransport, headers: headers}},
		}, nil
	}
	cmd := exec.Command(c.Command, c.Args...) //nolint:gosec,noctx // the user configured it; the transport stops it
	cmd.Dir = c.Cwd
	if cmd.Dir == "" {
		cmd.Dir = m.opts.Workspace
	}
	cmd.Env = m.env(c)
	cmd.Stderr = m.opts.Stderr
	if cmd.Stderr == nil {
		cmd.Stderr = io.Discard
	}

	return &sdk.CommandTransport{Command: cmd}, nil
}

// env is the stdio server's environment: the default variables, then
// env_vars by name, then env.
func (m *Manager) env(c ServerConfig) []string {
	vars := map[string]string{}
	for _, name := range append(slices.Clone(defaultEnv), c.EnvVars...) {
		if v := m.opts.Getenv(name); v != "" {
			vars[name] = v
		}
	}
	maps.Copy(vars, c.Env)
	out := make([]string, 0, len(vars))
	for _, k := range slices.Sorted(maps.Keys(vars)) {
		out = append(out, k+"="+vars[k])
	}

	return out
}

// headers are http_headers, env_http_headers, and the bearer token.
func (m *Manager) headers(c ServerConfig) (http.Header, error) {
	h := http.Header{}
	for k, v := range c.HTTPHeaders {
		h.Set(k, v)
	}
	for k, name := range c.EnvHTTPHeaders {
		if v := m.opts.Getenv(name); v != "" {
			h.Set(k, v)
		}
	}
	if name := c.BearerTokenEnvVar; name != "" {
		token := strings.TrimSpace(m.opts.Getenv(name))
		if token == "" {
			return nil, fmt.Errorf("the environment variable %s for the bearer token is not set", name)
		}
		h.Set("Authorization", "Bearer "+token)
	}

	return h, nil
}

// headerTransport adds fixed headers to every request.
type headerTransport struct {
	base    http.RoundTripper
	headers http.Header
}

func (t headerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	maps.Copy(r.Header, t.headers)

	return t.base.RoundTrip(r) //nolint:wrapcheck // a transport passes errors through
}
