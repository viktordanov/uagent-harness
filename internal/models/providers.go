package models

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"

	"github.com/unreallabsai/unreal-agent/harness/llm/clients/openaicodex"

	"github.com/viktordanov/uagent-harness/internal/engine/codexauth"
)

// codexOriginator is what the runner's openaicodex client sends as
// originator and User-Agent; the models request sends the same.
const codexOriginator = "unreal-agent"

// newCodexSource lists the ChatGPT backend's models for the Codex login, as
// Codex does for ChatGPT auth: GET {base}/models?client_version=…, with the
// headers the engine sends to {base}/responses.
func newCodexSource(base string, getenv func(string) string) (Source, error) {
	base, err := codexBaseURL(base)
	if err != nil {
		return nil, err
	}
	if getenv == nil {
		getenv = os.Getenv
	}
	config, err := openaicodex.EnvironmentConfig(getenv)
	if err != nil {
		return nil, fmt.Errorf("failed to find the Codex credentials: %w", err)
	}
	creds, err := codexauth.Load(config)
	if err != nil {
		return nil, fmt.Errorf("failed to read the Codex credentials: %w", err)
	}

	return httpSource{
		client:   noRedirects(),
		url:      base + "/models?" + url.Values{"client_version": {CodexClientVersion}}.Encode(),
		identity: identity(ProviderCodex, base, creds.AccountID),
		key:      creds.AccessToken,
		headers: map[string]string{
			"ChatGPT-Account-ID": creds.AccountID,
			"originator":         codexOriginator,
			"User-Agent":         codexOriginator,
		},
		parse: parseCodex,
	}, nil
}

// openAIList is the OpenAI-style list: OpenAI's /v1/models, OpenRouter's
// /api/v1/models (with context_length), and Fireworks' /inference/v1/models.
type openAIList struct {
	Data []struct {
		ID            string `json:"id"`
		Name          string `json:"name"`
		Description   string `json:"description"`
		ContextLength int64  `json:"context_length"`
		ContextWindow int64  `json:"context_window"`
		TopProvider   struct {
			ContextLength int64 `json:"context_length"`
		} `json:"top_provider"`
	} `json:"data"`
	// Models is set when the endpoint answers in Codex's shape (a gateway).
	Models json.RawMessage `json:"models"`
}

func parseOpenAI(body []byte) ([]Model, error) {
	var list openAIList
	if err := json.Unmarshal(body, &list); err != nil {
		return nil, fmt.Errorf("invalid models response: %w", err)
	}
	if list.Data == nil && list.Models != nil {
		return parseCodex(body)
	}
	out := make([]Model, 0, len(list.Data))
	for _, d := range list.Data {
		if d.ID == "" {
			continue
		}
		window := d.ContextLength
		if window == 0 {
			window = d.ContextWindow
		}
		if window == 0 {
			window = d.TopProvider.ContextLength
		}
		out = append(out, Model{ID: d.ID, DisplayName: d.Name, Description: d.Description, ContextWindow: window})
	}
	sortModels(out)

	return out, nil
}

// ollamaTags is Ollama's /api/tags: the models pulled locally.
type ollamaTags struct {
	Models []struct {
		Name  string `json:"name"`
		Model string `json:"model"`
	} `json:"models"`
}

func parseOllama(body []byte) ([]Model, error) {
	var tags ollamaTags
	if err := json.Unmarshal(body, &tags); err != nil {
		return nil, fmt.Errorf("invalid ollama tags response: %w", err)
	}
	out := make([]Model, 0, len(tags.Models))
	for _, t := range tags.Models {
		id := t.Model
		if id == "" {
			id = t.Name
		}
		if id != "" {
			out = append(out, Model{ID: id, DisplayName: t.Name})
		}
	}
	sortModels(out)

	return out, nil
}

// enrich fills metadata a list without it lacks from the bundled catalog,
// as Codex merges a non-authoritative list over its bundled models.
func enrich(provider string, list []Model) []Model {
	bundled := Bundled(provider)
	if bundled.Origin != OriginBundled {
		return list
	}
	for i, m := range list {
		if m.ContextWindow != 0 {
			continue
		}
		if b, ok, _ := bundled.Lookup(m.ID); ok {
			list[i] = b
		}
	}
	sortModels(list)

	return list
}
