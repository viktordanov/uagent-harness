// Package fakellm is a scripted OpenAI Responses API for tests. Point the
// runner's openai provider at URL (with any API key) and every model request
// gets the next Reply, so the process and embedded engines can be driven by
// the same script and compared.
package fakellm

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// Reply is one model response: Bash calls, then a message. A reply with
// commands is a tool turn; the message is the final answer otherwise.
type Reply struct {
	Text     string
	Commands []string
	// Gate, when set, holds the response until it is closed or the request
	// is canceled, so a test can act while the model is "thinking".
	Gate <-chan struct{}
}

// Request is what the harness sent, reduced to what tests check.
type Request struct {
	Model       string
	Effort      string
	ServiceTier string
	// UserTexts are the user messages in the request input, in order.
	UserTexts []string
	// System is the system prompt (the system messages in the input).
	System string
	// ToolOutputs are the tool results in the input, in order.
	ToolOutputs []string
}

// Server serves the script. When the script runs out, it answers "done".
type Server struct {
	URL string

	mu       sync.Mutex
	replies  []Reply
	requests []Request
	seen     chan int
}

// New starts a server that closes with the test.
func New(tb testing.TB, replies ...Reply) *Server {
	tb.Helper()
	s := &Server{replies: replies, seen: make(chan int, 1024)}
	srv := httptest.NewServer(http.HandlerFunc(s.serve))
	tb.Cleanup(srv.Close)
	s.URL = srv.URL

	return s
}

// Requests returns the requests so far.
func (s *Server) Requests() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]Request(nil), s.requests...)
}

// Seen receives the number of each request as it arrives (1 for the first).
func (s *Server) Seen() <-chan int { return s.seen }

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	if !strings.HasSuffix(r.URL.Path, "/responses") {
		http.NotFound(w, r)

		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)

		return
	}
	req := parseRequest(body)
	s.mu.Lock()
	s.requests = append(s.requests, req)
	n := len(s.requests)
	reply := Reply{Text: "done"}
	if len(s.replies) > 0 {
		reply, s.replies = s.replies[0], s.replies[1:]
	}
	s.mu.Unlock()
	s.seen <- n
	if reply.Gate != nil {
		select {
		case <-reply.Gate:
		case <-r.Context().Done():
			return
		}
	}
	event, err := json.Marshal(streamEvent{Type: "response.completed", Response: response(n, reply)})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)

		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	_, _ = fmt.Fprintf(w, "data: %s\n\n", event)
}

type (
	streamEvent struct {
		Type     string       `json:"type"`
		Response responseBody `json:"response"`
	}
	responseBody struct {
		ID     string       `json:"id"`
		Object string       `json:"object"`
		Status string       `json:"status"`
		Output []outputItem `json:"output"`
		Usage  usage        `json:"usage"`
	}
	outputItem struct {
		ID        string        `json:"id"`
		Type      string        `json:"type"`
		Status    string        `json:"status"`
		CallID    string        `json:"call_id,omitempty"`
		Name      string        `json:"name,omitempty"`
		Arguments string        `json:"arguments,omitempty"`
		Role      string        `json:"role,omitempty"`
		Phase     string        `json:"phase,omitempty"`
		Content   []contentPart `json:"content,omitempty"`
	}
	contentPart struct {
		Type        string `json:"type"`
		Text        string `json:"text"`
		Annotations []any  `json:"annotations"`
		Logprobs    []any  `json:"logprobs"`
	}
	usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
		TotalTokens  int `json:"total_tokens"`
	}
)

const completed = "completed"

func response(n int, reply Reply) responseBody {
	output := []outputItem{}
	for i, cmd := range reply.Commands {
		args, err := json.Marshal(struct {
			Command string `json:"command"`
		}{cmd})
		if err != nil {
			panic(err) // a struct of one string always encodes
		}
		output = append(output, outputItem{
			ID: fmt.Sprintf("fc-%d-%d", n, i), Type: "function_call", Status: completed,
			CallID: fmt.Sprintf("call-%d-%d", n, i), Name: "Bash", Arguments: string(args),
		})
	}
	if reply.Text != "" {
		phase := "final_answer"
		if len(reply.Commands) > 0 {
			phase = "commentary"
		}
		output = append(output, outputItem{
			ID: fmt.Sprintf("msg-%d", n), Type: "message", Role: "assistant", Status: completed, Phase: phase,
			Content: []contentPart{{Type: "output_text", Text: reply.Text, Annotations: []any{}, Logprobs: []any{}}},
		})
	}

	return responseBody{
		ID: fmt.Sprintf("resp-%d", n), Object: "response", Status: completed, Output: output,
		Usage: usage{InputTokens: 100 * n, OutputTokens: 10, TotalTokens: 100*n + 10},
	}
}

func parseRequest(body []byte) Request {
	var raw struct {
		Model       string `json:"model"`
		ServiceTier string `json:"service_tier"`
		Reasoning   struct {
			Effort string `json:"effort"`
		} `json:"reasoning"`
		Input []struct {
			Type    string          `json:"type"`
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
			Output  json.RawMessage `json:"output"`
		} `json:"input"`
	}
	_ = json.Unmarshal(body, &raw)
	req := Request{Model: raw.Model, ServiceTier: raw.ServiceTier, Effort: raw.Reasoning.Effort}
	for _, in := range raw.Input {
		if in.Type == "function_call_output" {
			req.ToolOutputs = append(req.ToolOutputs, strings.Join(texts(in.Output), ""))

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
