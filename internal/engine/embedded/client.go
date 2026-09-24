package embedded

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/unreallabsai/unreal-agent/harness/llm/responsesapi"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/engine"
)

// client resolves the provider, model, and credentials as the runner does
// and returns the model and the switching adapter.
func (w *wiring) client(req core.Request, opts engine.Options) (string, *switcher, error) {
	p, err := w.e.provider(req.Provider)
	if err != nil {
		return "", nil, err
	}
	baseURL := strings.TrimSpace(req.BaseURL)
	if baseURL == "" {
		baseURL = p.BaseURL
	}
	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = p.DefaultModel
	}
	if model == "" {
		return "", nil, errors.New("the model must be set for provider " + p.Name)
	}
	apiKey, err := w.apiKey(p)
	if err != nil {
		return "", nil, err
	}
	maxAttempts, err := w.maxAttempts(req)
	if err != nil {
		return "", nil, err
	}
	sw, err := newSwitcher(model, opts.ServiceTier == tierPriority, func(priority bool) (Client, error) {
		if priority && !p.Priority {
			return nil, errNoPriority
		}
		c, err := p.NewClient(ClientConfig{APIKey: apiKey, BaseURL: baseURL, MaxAttempts: maxAttempts, Priority: priority, Getenv: w.getenv})
		if err != nil {
			return nil, fmt.Errorf("failed to create the %s client: %w", p.Name, err)
		}

		return c, nil
	})

	return model, sw, err
}

// apiKey returns UNREAL_HARNESS_LLM_API_KEY, else the provider's key
// variable. A provider without a key variable needs no key.
func (w *wiring) apiKey(p Provider) (string, error) {
	if p.APIKeyEnv == "" {
		return "", nil
	}
	key := strings.TrimSpace(w.getenv("UNREAL_HARNESS_LLM_API_KEY"))
	if key == "" {
		key = strings.TrimSpace(w.getenv(p.APIKeyEnv))
	}
	if key == "" {
		return "", fmt.Errorf("UNREAL_HARNESS_LLM_API_KEY or %s must be set", p.APIKeyEnv)
	}

	return key, nil
}

// maxAttempts returns the request's attempt limit, else the environment's,
// else the runner's default.
func (w *wiring) maxAttempts(req core.Request) (int, error) {
	n := responsesapi.DefaultMaxAttempts
	if req.MaxAttempts > 0 {
		n = req.MaxAttempts
	} else if v := strings.TrimSpace(w.getenv("UNREAL_HARNESS_LLM_MAX_ATTEMPTS")); v != "" {
		parsed, err := strconv.Atoi(v)
		if err != nil || parsed <= 0 {
			return 0, fmt.Errorf("invalid UNREAL_HARNESS_LLM_MAX_ATTEMPTS %q", v)
		}
		n = parsed
	}

	return n, nil
}
