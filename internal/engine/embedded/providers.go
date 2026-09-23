package embedded

import (
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/unreallabsai/unreal-agent/harness/llm"
	"github.com/unreallabsai/unreal-agent/harness/llm/clients/fireworks"
	"github.com/unreallabsai/unreal-agent/harness/llm/clients/ollama"
	"github.com/unreallabsai/unreal-agent/harness/llm/clients/openai"
	"github.com/unreallabsai/unreal-agent/harness/llm/clients/openaicodex"
	"github.com/unreallabsai/unreal-agent/harness/llm/clients/openrouter"
	"github.com/unreallabsai/unreal-agent/harness/llm/responsesapi"
	"github.com/unreallabsai/unreal-agent/harness/primitives"
)

// Client is a model client the engine can close.
type Client interface {
	llm.Adapter
	Close() error
}

// ClientConfig is what a provider needs to build a client.
type ClientConfig struct {
	APIKey      string
	BaseURL     string
	MaxAttempts int
	// Priority asks for priority processing (service_tier "priority").
	Priority bool
	Getenv   func(string) string
}

// Provider mirrors unreal-agent-runner v0.1.1's provider table
// (cmd/internal/agentrunner/providers.go).
type Provider struct {
	Name         string
	BaseURL      string
	DefaultModel string
	// APIKeyEnv is empty when the client finds its own credentials.
	APIKeyEnv string
	// Priority says whether the provider accepts service_tier "priority".
	Priority  bool
	NewClient func(ClientConfig) (Client, error)
}

var errNoPriority = errors.New("this provider has no priority processing")

// DefaultProviders are the runner's providers.
func DefaultProviders() []Provider {
	return []Provider{
		{
			Name: "ollama", BaseURL: ollama.BaseURL,
			NewClient: func(c ClientConfig) (Client, error) {
				if c.Priority {
					return nil, errNoPriority
				}

				return ollama.NewClient(ollama.Config{BaseURL: c.BaseURL, MaxAttempts: &c.MaxAttempts})
			},
		},
		{
			Name: "openai", BaseURL: "https://api.openai.com/v1", DefaultModel: "gpt-6-astra",
			APIKeyEnv: "OPENAI_API_KEY", Priority: true,
			NewClient: func(c ClientConfig) (Client, error) {
				if c.Priority {
					return priorityClient(primitives.NewRemoteClient(), strings.TrimRight(c.BaseURL, "/")+"/responses", c.MaxAttempts,
						map[string][]string{"Authorization": {"Bearer " + c.APIKey}, "Content-Type": {"application/json"}},
						responsesapi.CacheKeyPlacement{UsePromptCacheKeyField: true})
				}

				return openai.NewClient(openai.Config{APIKey: c.APIKey, BaseURL: c.BaseURL, MaxAttempts: &c.MaxAttempts})
			},
		},
		{
			Name: "openai-codex", BaseURL: openaicodex.BaseURL, Priority: true,
			NewClient: func(c ClientConfig) (Client, error) {
				config, err := openaicodex.EnvironmentConfig(c.Getenv)
				if err != nil {
					return nil, err
				}
				config.BaseURL, config.MaxAttempts = c.BaseURL, &c.MaxAttempts
				if c.Priority {
					return codexPriorityClient(config)
				}

				return openaicodex.NewClient(config)
			},
		},
		{
			Name: "openrouter", BaseURL: "https://openrouter.ai/api/v1", APIKeyEnv: "OPENROUTER_API_KEY",
			NewClient: func(c ClientConfig) (Client, error) {
				if c.Priority {
					return nil, errNoPriority
				}

				return openrouter.NewClient(openrouter.Config{APIKey: c.APIKey, BaseURL: c.BaseURL, MaxAttempts: &c.MaxAttempts})
			},
		},
		{
			Name: "fireworks", BaseURL: "https://api.fireworks.ai/inference/v1", APIKeyEnv: "FIREWORKS_API_KEY",
			NewClient: func(c ClientConfig) (Client, error) {
				if c.Priority {
					return nil, errNoPriority
				}

				return fireworks.NewClient(fireworks.Config{APIKey: c.APIKey, BaseURL: c.BaseURL, MaxAttempts: &c.MaxAttempts})
			},
		},
	}
}

// codexPriorityClient is openaicodex.NewClient with service_tier "priority":
// the same endpoint, headers, and redirect rule.
func codexPriorityClient(config openaicodex.Config) (Client, error) {
	creds, err := codexCredentials(config)
	if err != nil {
		return nil, err
	}
	baseURL := strings.TrimRight(strings.TrimSpace(config.BaseURL), "/")
	if baseURL == "" {
		baseURL = openaicodex.BaseURL
	}
	transport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, errors.New("the default HTTP transport is not an *http.Transport")
	}
	// Never forward subscription credentials through redirects.
	remote := primitives.NewRemoteClientWithHTTPClient(&http.Client{
		Transport:     transport.Clone(),
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	})

	return priorityClient(remote, baseURL+"/responses", *config.MaxAttempts, map[string][]string{
		"Authorization":      {"Bearer " + creds.accessToken},
		"ChatGPT-Account-ID": {creds.accountID},
		"Content-Type":       {"application/json"},
		"originator":         {"unreal-agent"},
		"User-Agent":         {"unreal-agent"},
	}, responsesapi.CacheKeyPlacement{UsePromptCacheKeyField: true, Header: "session-id"})
}

func priorityClient(remote *primitives.RemoteClient, endpoint string, maxAttempts int, headers map[string][]string, cache responsesapi.CacheKeyPlacement) (Client, error) {
	adapter, err := responsesapi.NewAdapter(remote, responsesapi.Config{
		Endpoint: endpoint, Headers: headers, CacheKeyPlacement: cache, MaxAttempts: &maxAttempts,
		Extensions: map[string]jsontext.Value{"service_tier": jsontext.Value(`"priority"`)},
	})
	if err != nil {
		_ = remote.Close()

		return nil, fmt.Errorf("failed to create the priority client: %w", err)
	}

	return remoteAdapter{Adapter: adapter, remote: remote}, nil
}

type remoteAdapter struct {
	llm.Adapter

	remote *primitives.RemoteClient
}

func (a remoteAdapter) Close() error { return a.remote.Close() }
