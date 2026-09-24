package embedded

// The providers' Responses clients, built as unreal-agent-runner v0.1.1
// builds them (harness/llm/clients/*/client.go and the HTTP client of
// harness/primitives/remote.go, MIT License, Copyright (c) 2026 Unreal
// Labs; see THIRD_PARTY_NOTICES.md) but over an *http.Client uah makes,
// whose transport sees each attempt of a model request (reconnect.go). The
// runner's constructors make their own and take no transport. The request
// each client sends is the runner's (TestClients_MatchTheRunner).

import (
	"context"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"maps"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/unreallabsai/unreal-agent/harness/llm"
	"github.com/unreallabsai/unreal-agent/harness/llm/clients/fireworks"
	"github.com/unreallabsai/unreal-agent/harness/llm/clients/ollama"
	"github.com/unreallabsai/unreal-agent/harness/llm/clients/openaicodex"
	"github.com/unreallabsai/unreal-agent/harness/llm/responsesapi"
	"github.com/unreallabsai/unreal-agent/harness/primitives"

	"github.com/viktordanov/uagent-harness/internal/engine/codexauth"
)

const (
	headerContentType = "Content-Type"
	contentJSON       = "application/json"
)

// remoteHTTPClient is primitives.NewRemoteClient's HTTP client with the
// watching transport.
func remoteHTTPClient() *http.Client {
	dialer := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}

	return &http.Client{Transport: watchTransport{base: &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: time.Second,
	}}}
}

// codexHTTPClient is openaicodex.NewClient's HTTP client with the watching
// transport: a clone of the default transport that never forwards
// subscription credentials through redirects.
func codexHTTPClient() (*http.Client, error) {
	transport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, errors.New("the default HTTP transport is not an *http.Transport")
	}

	return &http.Client{
		Transport:     watchTransport{base: transport.Clone()},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}, nil
}

// newClient builds a Responses client over hc with the attempt limit and,
// for priority processing, service_tier "priority".
func newClient(hc *http.Client, c ClientConfig, config responsesapi.Config) (remoteAdapter, error) {
	remote := primitives.NewRemoteClientWithHTTPClient(hc)
	config.MaxAttempts = &c.MaxAttempts
	if c.Priority {
		config.Extensions = maps.Clone(config.Extensions)
		if config.Extensions == nil {
			config.Extensions = map[string]jsontext.Value{}
		}
		config.Extensions["service_tier"] = jsontext.Value(`"priority"`)
	}
	adapter, err := responsesapi.NewAdapter(remote, config)
	if err != nil {
		_ = remote.Close()

		return remoteAdapter{}, err //nolint:wrapcheck // the provider wraps it
	}

	return remoteAdapter{Adapter: adapter, remote: remote}, nil
}

// keyedClient is the client of a provider with an API key: openai,
// openrouter, and fireworks, which name themselves in their errors.
func keyedClient(name string, c ClientConfig, cache responsesapi.CacheKeyPlacement, extensions map[string]jsontext.Value) (remoteAdapter, error) {
	if strings.TrimSpace(c.APIKey) == "" {
		return remoteAdapter{}, errors.New(name + " API key must be set")
	}
	baseURL := strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	if baseURL == "" {
		return remoteAdapter{}, errors.New(name + " base URL must be set")
	}

	return newClient(remoteHTTPClient(), c, responsesapi.Config{
		Endpoint:          baseURL + "/responses",
		Headers:           map[string][]string{"Authorization": {"Bearer " + c.APIKey}, headerContentType: {contentJSON}},
		CacheKeyPlacement: cache,
		Extensions:        extensions,
	})
}

func openaiClient(c ClientConfig) (Client, error) {
	return keyedClient("OpenAI", c, responsesapi.CacheKeyPlacement{UsePromptCacheKeyField: true}, nil)
}

// openrouterClient asks for OpenRouter's one-hour prompt caching, as the
// runner's client does.
func openrouterClient(c ClientConfig) (Client, error) {
	return keyedClient("OpenRouter", c, responsesapi.CacheKeyPlacement{Header: "x-session-id"},
		map[string]jsontext.Value{"cache_control": jsontext.Value(`{"type":"ephemeral","ttl":"1h"}`)})
}

// fireworksClient keeps the runner's client's usage conversion, which
// wraps any adapter.
func fireworksClient(c ClientConfig) (Client, error) {
	ra, err := keyedClient("fireworks", c, responsesapi.CacheKeyPlacement{Header: "x-session-affinity"}, nil)
	if err != nil {
		return nil, err
	}

	return remoteAdapter{Adapter: &fireworks.Client{Adapter: ra.Adapter}, remote: ra.remote}, nil
}

func ollamaClient(c ClientConfig) (Client, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	if baseURL == "" {
		baseURL = ollama.BaseURL
	}

	return newClient(remoteHTTPClient(), c, responsesapi.Config{
		Endpoint: baseURL + "/responses",
		Headers:  map[string][]string{headerContentType: {contentJSON}},
	})
}

// codexClient is the openai-codex client: ChatGPT subscription credentials
// from the environment or Codex's auth file.
func codexClient(c ClientConfig) (Client, error) {
	config, err := openaicodex.EnvironmentConfig(c.Getenv)
	if err != nil {
		return nil, err //nolint:wrapcheck // the provider wraps it
	}
	baseURL, err := codexBaseURL(c.BaseURL)
	if err != nil {
		return nil, err
	}
	if c.MaxAttempts <= 0 {
		return nil, errors.New("max attempts must be positive")
	}
	creds, err := codexauth.Load(config)
	if err != nil {
		return nil, err //nolint:wrapcheck // the provider wraps it
	}
	hc, err := codexHTTPClient()
	if err != nil {
		return nil, err
	}
	ra, err := newClient(hc, c, responsesapi.Config{
		Endpoint: baseURL + "/responses",
		Headers: map[string][]string{
			"Authorization":      {"Bearer " + creds.AccessToken},
			"ChatGPT-Account-ID": {creds.AccountID},
			headerContentType:    {contentJSON},
			"originator":         {"unreal-agent"},
			"User-Agent":         {"unreal-agent"},
		},
		CacheKeyPlacement: responsesapi.CacheKeyPlacement{UsePromptCacheKeyField: true, Header: "session-id"},
	})
	if err != nil {
		return nil, err
	}

	return codexAdapter{ra}, nil
}

// codexBaseURL accepts the ChatGPT backend or a loopback endpoint, as the
// runner's client does.
func codexBaseURL(value string) (string, error) {
	value = strings.TrimRight(strings.TrimSpace(value), "/")
	if value == "" || value == openaicodex.BaseURL {
		return openaicodex.BaseURL, nil
	}
	parsed, err := url.Parse(value)
	if err == nil && parsed.User == nil && parsed.RawQuery == "" && parsed.Fragment == "" &&
		(parsed.Scheme == "http" || parsed.Scheme == "https") {
		if ip := net.ParseIP(parsed.Hostname()); ip != nil && ip.IsLoopback() {
			return value, nil
		}
	}

	return "", errors.New("codex base URL must be " + openaicodex.BaseURL + " or an explicit loopback IP endpoint")
}

// codexAdapter checks and explains as openaicodex.Client.Respond does.
type codexAdapter struct{ remoteAdapter }

func (a codexAdapter) Respond(ctx context.Context, req llm.Request, opts llm.RequestOptions) (llm.Response, error) {
	if err := ctx.Err(); err != nil {
		return llm.Response{}, err //nolint:wrapcheck // as the runner's client
	}
	if req.Model.MaxOutputTokens != nil {
		return llm.Response{}, errors.New("codex does not support max_output_tokens")
	}
	resp, err := a.Adapter.Respond(ctx, req, opts)
	if apiErr, ok := errors.AsType[*responsesapi.APIError](err); ok && apiErr.StatusCode == http.StatusUnauthorized {
		return llm.Response{}, fmt.Errorf("codex credentials rejected; renew them externally and recreate the client: %w", err)
	}

	return resp, err //nolint:wrapcheck // the coordinator wraps model errors
}
