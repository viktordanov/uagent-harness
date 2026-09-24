package models

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/unreallabsai/unreal-agent/harness/llm/clients/ollama"
	"github.com/unreallabsai/unreal-agent/harness/llm/clients/openaicodex"
)

// The providers uah runs, as the engine names them.
const (
	ProviderCodex      = "openai-codex"
	ProviderOpenAI     = "openai"
	ProviderOpenRouter = "openrouter"
	ProviderFireworks  = "fireworks"
	ProviderOllama     = "ollama"
)

// EnvAPIKey overrides every provider's key variable, as in the engine.
const EnvAPIKey = "UNREAL_HARNESS_LLM_API_KEY"

// Endpoint is how uah reaches a provider: the engine's provider table
// (internal/engine/embedded/providers.go), which a test keeps in step.
type Endpoint struct {
	BaseURL string
	// KeyEnv is the API key variable; empty when the provider needs none or
	// finds its own credentials.
	KeyEnv string
	// KeyOptional lists without a key (the list is public).
	KeyOptional bool
}

// Endpoints are the providers' default base URLs and key variables.
var Endpoints = map[string]Endpoint{
	ProviderCodex:      {BaseURL: openaicodex.BaseURL},
	ProviderOpenAI:     {BaseURL: "https://api.openai.com/v1", KeyEnv: "OPENAI_API_KEY"},
	ProviderOpenRouter: {BaseURL: "https://openrouter.ai/api/v1", KeyEnv: "OPENROUTER_API_KEY", KeyOptional: true},
	ProviderFireworks:  {BaseURL: "https://api.fireworks.ai/inference/v1", KeyEnv: "FIREWORKS_API_KEY"},
	ProviderOllama:     {BaseURL: ollama.BaseURL},
}

// Source lists one provider's models for one login.
type Source interface {
	// Identity is a hash of the provider, base URL, and account or key; it
	// changes when any of them does and never holds a secret.
	Identity() string
	// Fetch asks the provider for its list. With the ETag of a cached list,
	// the provider may answer that it did not change (NotModified).
	Fetch(ctx context.Context, etag string) (Fetched, error)
}

// Fetched is a provider's answer.
type Fetched struct {
	Models      []Model
	ETag        string
	NotModified bool
}

// ErrUnsupported means uah has no model list for the provider.
var ErrUnsupported = errors.New("unsupported provider")

// NewSource builds the provider's source with the engine's configuration:
// the base URL (empty for the provider's default) and the credentials the
// environment selects (UNREAL_HARNESS_LLM_API_KEY, the provider's key
// variable, or for openai-codex the OPENAI_CODEX_* variables and the Codex
// auth file). It reads credentials but makes no request.
func NewSource(provider, baseURL string, getenv func(string) string) (Source, error) {
	ep, ok := Endpoints[provider]
	if !ok {
		return nil, fmt.Errorf("%w %q", ErrUnsupported, provider)
	}
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		base = ep.BaseURL
	}
	if provider == ProviderCodex {
		return newCodexSource(base, getenv)
	}
	key, err := apiKey(ep, getenv)
	if err != nil {
		return nil, err
	}
	s := httpSource{client: noRedirects(), identity: identity(provider, base, key), key: key}
	switch provider {
	case ProviderOllama:
		s.url, s.parse = strings.TrimSuffix(base, "/v1")+"/api/tags", parseOllama
	default:
		s.url, s.parse = base+"/models", parseOpenAI
	}

	return s, nil
}

func apiKey(ep Endpoint, getenv func(string) string) (string, error) {
	if ep.KeyEnv == "" {
		return "", nil
	}
	key := strings.TrimSpace(getenv(EnvAPIKey))
	if key == "" {
		key = strings.TrimSpace(getenv(ep.KeyEnv))
	}
	if key == "" && !ep.KeyOptional {
		return "", fmt.Errorf("%s or %s must be set", EnvAPIKey, ep.KeyEnv)
	}

	return key, nil
}

// identity hashes what scopes a list, so the cache never holds a secret.
func identity(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))

	return hex.EncodeToString(sum[:16])
}

// noRedirects is a client that never forwards credentials through a redirect.
func noRedirects() *http.Client {
	return &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

// codexBaseURL accepts the ChatGPT backend or a loopback test server, as the
// runner's openaicodex client does, so the token goes nowhere else.
func codexBaseURL(value string) (string, error) {
	if value == openaicodex.BaseURL {
		return value, nil
	}
	u, err := url.Parse(value)
	if err == nil && u.User == nil && u.RawQuery == "" && u.Fragment == "" && (u.Scheme == "http" || u.Scheme == "https") {
		if ip := net.ParseIP(u.Hostname()); ip != nil && ip.IsLoopback() {
			return value, nil
		}
	}

	return "", errors.New("codex base URL must be " + openaicodex.BaseURL + " or an explicit loopback IP endpoint")
}

// httpSource is a GET that returns a list.
type httpSource struct {
	client   *http.Client
	url      string
	identity string
	key      string
	headers  map[string]string
	parse    func([]byte) ([]Model, error)
}

func (s httpSource) Identity() string { return s.identity }

// maxBody bounds a models response.
const maxBody = 16 << 20

func (s httpSource) Fetch(ctx context.Context, etag string) (Fetched, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.url, http.NoBody)
	if err != nil {
		return Fetched{}, fmt.Errorf("failed to build the models request: %w", err)
	}
	if s.key != "" {
		req.Header.Set("Authorization", "Bearer "+s.key)
	}
	for k, v := range s.headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("Accept", "application/json")
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return Fetched{}, fmt.Errorf("failed to list models: %w", redact(err, s.url))
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotModified {
		return Fetched{ETag: etag, NotModified: true}, nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return Fetched{}, fmt.Errorf("failed to read the models list: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return Fetched{}, fmt.Errorf("the models list returned %s", resp.Status)
	}
	models, err := s.parse(body)
	if err != nil {
		return Fetched{}, err
	}

	return Fetched{Models: models, ETag: resp.Header.Get("ETag")}, nil
}

// redact keeps a transport error but not the URL's query.
func redact(err error, raw string) error {
	if ue, ok := errors.AsType[*url.Error](err); ok {
		return fmt.Errorf("%s %s: %w", ue.Op, strings.SplitN(raw, "?", 2)[0], ue.Err)
	}

	return err
}
