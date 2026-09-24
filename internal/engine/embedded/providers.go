package embedded

import (
	"errors"

	"github.com/unreallabsai/unreal-agent/harness/llm"
	"github.com/unreallabsai/unreal-agent/harness/llm/clients/ollama"
	"github.com/unreallabsai/unreal-agent/harness/llm/clients/openaicodex"
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

// DefaultProviders are the runner's providers, with clients whose retries
// the engine can watch (clients.go).
func DefaultProviders() []Provider {
	return []Provider{
		{Name: "ollama", BaseURL: ollama.BaseURL, NewClient: noPriority(ollamaClient)},
		{
			Name: "openai", BaseURL: "https://api.openai.com/v1", DefaultModel: "gpt-6-astra",
			APIKeyEnv: "OPENAI_API_KEY", Priority: true, NewClient: openaiClient,
		},
		{Name: "openai-codex", BaseURL: openaicodex.BaseURL, Priority: true, NewClient: codexClient},
		{Name: "openrouter", BaseURL: "https://openrouter.ai/api/v1", APIKeyEnv: "OPENROUTER_API_KEY", NewClient: noPriority(openrouterClient)},
		{Name: "fireworks", BaseURL: "https://api.fireworks.ai/inference/v1", APIKeyEnv: "FIREWORKS_API_KEY", NewClient: noPriority(fireworksClient)},
	}
}

// noPriority refuses priority processing, for a provider without it.
func noPriority(build func(ClientConfig) (Client, error)) func(ClientConfig) (Client, error) {
	return func(c ClientConfig) (Client, error) {
		if c.Priority {
			return nil, errNoPriority
		}

		return build(c)
	}
}

// remoteAdapter is a Responses adapter and the remote client it closes.
type remoteAdapter struct {
	llm.Adapter

	remote *primitives.RemoteClient
}

func (a remoteAdapter) Close() error { return a.remote.Close() }
