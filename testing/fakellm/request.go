package fakellm

import (
	"encoding/json"
	"strings"
)

func parseRequest(body []byte) Request {
	var raw struct {
		Model       string `json:"model"`
		ServiceTier string `json:"service_tier"`
		CacheKey    string `json:"prompt_cache_key"`
		Reasoning   struct {
			Effort string `json:"effort"`
		} `json:"reasoning"`
		Tools []struct {
			Name       string          `json:"name"`
			Parameters json.RawMessage `json:"parameters"`
		} `json:"tools"`
		Input []struct {
			Type    string          `json:"type"`
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
			Output  json.RawMessage `json:"output"`
			CallID  string          `json:"call_id"`
		} `json:"input"`
	}
	var items struct {
		Input []json.RawMessage `json:"input"`
		Tools []json.RawMessage `json:"tools"`
	}
	_ = json.Unmarshal(body, &raw)
	_ = json.Unmarshal(body, &items)
	req := Request{Model: raw.Model, ServiceTier: raw.ServiceTier, Effort: raw.Reasoning.Effort, Tools: map[string]string{}, Input: items.Input, ToolDefs: items.Tools, CacheKey: raw.CacheKey}
	for _, t := range raw.Tools {
		req.Tools[t.Name] = string(t.Parameters)
		req.ToolNames = append(req.ToolNames, t.Name)
	}
	for _, in := range raw.Input {
		if in.Type == "function_call_output" {
			req.ToolOutputs = append(req.ToolOutputs, strings.Join(texts(in.Output), ""))
			req.ToolImages = append(req.ToolImages, images(in.Output)...)

			continue
		}
		if in.Type == "function_call" {
			req.CallIDs = append(req.CallIDs, in.CallID)

			continue
		}
		switch in.Role {
		case "user":
			req.UserTexts = append(req.UserTexts, texts(in.Content)...)
		case "system", "developer":
			req.System += strings.Join(texts(in.Content), "\n")
		}
	}

	return req
}

// images reads the image URLs in content parts.
func images(content json.RawMessage) []string {
	var parts []struct {
		ImageURL string `json:"image_url"`
	}
	_ = json.Unmarshal(content, &parts)
	var out []string
	for _, p := range parts {
		if p.ImageURL != "" {
			out = append(out, p.ImageURL)
		}
	}

	return out
}

// texts reads message content: a string, or parts with text.
func texts(content json.RawMessage) []string {
	var text string
	if json.Unmarshal(content, &text) == nil {
		return []string{text}
	}
	var parts []struct {
		Text string `json:"text"`
	}
	_ = json.Unmarshal(content, &parts)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		out = append(out, p.Text)
	}

	return out
}
