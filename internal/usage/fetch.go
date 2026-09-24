package usage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/unreallabsai/unreal-agent/harness/llm/clients/openaicodex"

	"github.com/viktordanov/uagent-harness/internal/engine/codexauth"
)

// Options configure Fetch. The zero value reads the real backend.
type Options struct {
	// BaseURL is the openai-codex provider's base URL; empty is the runner's
	// openaicodex.BaseURL. Only that URL or a loopback test server is
	// accepted, so the token goes nowhere else.
	BaseURL string
	// Client defaults to a client that never follows redirects.
	Client *http.Client
	// Now defaults to time.Now.
	Now func() time.Time
}

// ErrUnauthorized means the backend rejected the credentials.
var ErrUnauthorized = errors.New("the ChatGPT backend rejected the Codex credentials; sign in to Codex again")

// originator is what the runner's openaicodex client sends as originator and
// User-Agent (unreal-agent v0.1.1, harness/llm/clients/openaicodex/client.go:59-60).
const originator = "unreal-agent"

// maxBody bounds a usage response.
const maxBody = 1 << 20

// Fetch reads the subscription's usage with one GET, as Codex's /status does
// (backend-client/src/client/rate_limit_resets.rs:69-80 and 124-129). Errors
// never include a secret or the response body.
func Fetch(ctx context.Context, creds codexauth.Creds, opts Options) (Snapshot, error) {
	endpoint, err := URL(opts.BaseURL)
	if err != nil {
		return Snapshot{}, err
	}
	if !codexauth.HeaderValue(creds.AccessToken) || !codexauth.HeaderValue(creds.AccountID) {
		return Snapshot{}, errors.New("the Codex credentials are missing or invalid")
	}
	client := opts.Client
	if client == nil {
		client = &http.Client{
			Timeout:       30 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		}
	}
	now := time.Now
	if opts.Now != nil {
		now = opts.Now
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return Snapshot{}, fmt.Errorf("failed to build the usage request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+creds.AccessToken)
	req.Header.Set("Chatgpt-Account-Id", creds.AccountID)
	req.Header.Set("Originator", originator)
	req.Header.Set("User-Agent", originator)
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return Snapshot{}, fmt.Errorf("failed to read the usage: %w", err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return Snapshot{}, ErrUnauthorized
	case resp.StatusCode != http.StatusOK:
		return Snapshot{}, fmt.Errorf("the usage request returned %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return Snapshot{}, fmt.Errorf("failed to read the usage response: %w", err)
	}
	if len(body) > maxBody {
		return Snapshot{}, errors.New("the usage response is larger than 1 MiB")
	}

	return Parse(body, now())
}

// URL returns the usage endpoint for the provider's base URL. Codex picks the
// path by the base: /wham/usage under /backend-api, else /api/codex/usage
// (backend-client/src/client.rs:125-140).
func URL(base string) (string, error) {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" || base == openaicodex.BaseURL {
		return strings.TrimSuffix(openaicodex.BaseURL, "/codex") + "/wham/usage", nil
	}
	u, err := url.Parse(base)
	if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", errors.New("invalid codex base URL")
	}
	if ip := net.ParseIP(u.Hostname()); ip == nil || !ip.IsLoopback() {
		return "", errors.New("codex base URL must be " + openaicodex.BaseURL + " or an explicit loopback IP endpoint")
	}
	root := strings.TrimSuffix(base, "/codex")
	if strings.Contains(root, "/backend-api") {
		return root + "/wham/usage", nil
	}

	return root + "/api/codex/usage", nil
}
