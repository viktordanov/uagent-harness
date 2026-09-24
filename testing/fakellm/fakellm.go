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
	"slices"
	"strings"
	"sync"
	"testing"
)

// Reply is one model response: tool calls, then a message. A reply with
// calls is a tool turn; the message is the final answer otherwise.
type Reply struct {
	Text     string
	Commands []string
	// Escalated are Bash calls that ask to run outside the sandbox, with
	// sandbox_permissions "require_escalated" and a justification.
	Escalated []string
	// Calls are function calls to any tool, after the Bash calls.
	Calls []Call
	// Gate, when set, holds the response until it is closed or the request
	// is canceled, so a test can act while the model is "thinking".
	Gate <-chan struct{}
	// InputTokens, when set, is the usage the response reports as input
	// tokens (default 100 per request so far).
	InputTokens int
	// From, when set, builds the reply from the request, such as a call
	// that needs an ID from an earlier tool result.
	From func(Request) Reply
	// Fail, when set, is the HTTP status of an error answer with FailCode
	// as the Responses API error code, as a provider rejects a request.
	Fail     int
	FailCode string
	// FailBody, when set, is the error answer's body as sent, such as the
	// ChatGPT backend's {"detail":"..."}.
	FailBody string
	// NoUsage leaves the usage out of the response, as some providers do.
	NoUsage bool
	// Drop closes the connection without an answer, as a lost network
	// does; Cut closes it halfway through the stream. The client retries
	// either, so the next reply answers the same request.
	Drop bool
	Cut  bool
}

// Call is a function call to a tool by name, with JSON arguments.
type Call struct {
	Name string
	Args string
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
	// CallIDs are the tool calls in the input, in order.
	CallIDs []string
	// ToolImages are the image URLs in the tool results, in order.
	ToolImages []string
	// Tools are the offered tools' parameter schemas as JSON, by name.
	Tools map[string]string
	// ToolNames are the offered tools' names in order, and ToolDefs the
	// tools as sent.
	ToolNames []string
	ToolDefs  []json.RawMessage
	// Input are the input items as sent, in order.
	Input []json.RawMessage
	// CacheKey is the prompt cache key.
	CacheKey string
}

// Server serves the script. When the script runs out, it answers "done".
type Server struct {
	URL string

	mu       sync.Mutex
	replies  []Reply
	routes   []route
	requests []Request
	seen     chan int
}

// route is a script for the requests whose user messages contain match.
type route struct {
	match   string
	replies []Reply
}

// Route answers the requests whose user messages contain match with its
// own replies, then "done", so one server can serve a parent session and
// the subagents it starts with those messages.
func (s *Server) Route(match string, replies ...Reply) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.routes = append(s.routes, route{match: match, replies: replies})
}

// next takes the reply for req from its route or the main script. It holds
// s.mu.
func (s *Server) next(req Request) Reply {
	script := &s.replies
	for i, r := range s.routes {
		if slices.ContainsFunc(req.UserTexts, func(t string) bool { return strings.Contains(t, r.match) }) {
			script = &s.routes[i].replies

			break
		}
	}
	reply := Reply{Text: "done"}
	if len(*script) > 0 {
		reply, *script = (*script)[0], (*script)[1:]
	}

	return reply
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
	reply := s.next(req)
	s.mu.Unlock()
	if reply.From != nil {
		reply = reply.From(req)
	}
	s.seen <- n
	if reply.Gate != nil {
		select {
		case <-reply.Gate:
		case <-r.Context().Done():
			return
		}
	}
	if reply.Drop {
		drop(w)

		return
	}
	if reply.Fail != 0 {
		failWith(w, reply)

		return
	}
	event, err := json.Marshal(streamEvent{Type: "response.completed", Response: response(n, reply)})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)

		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	if reply.Cut {
		_, _ = fmt.Fprintf(w, "data: %s", event[:len(event)/2])
		http.NewResponseController(w).Flush() //nolint:errcheck // the connection closes next
		drop(w)

		return
	}
	_, _ = fmt.Fprintf(w, "data: %s\n\n", event)
}

// drop closes the request's connection at once.
func drop(w http.ResponseWriter) {
	conn, _, err := http.NewResponseController(w).Hijack()
	if err != nil {
		panic(err) // httptest's server supports hijacking
	}
	_ = conn.Close()
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
		Usage  *usage       `json:"usage,omitempty"`
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
	type bashArgs struct {
		Command       string `json:"command"`
		Permissions   string `json:"sandbox_permissions,omitempty"`
		Justification string `json:"justification,omitempty"`
	}
	calls := make([]bashArgs, 0, len(reply.Commands)+len(reply.Escalated))
	for _, cmd := range reply.Commands {
		calls = append(calls, bashArgs{Command: cmd})
	}
	for _, cmd := range reply.Escalated {
		calls = append(calls, bashArgs{Command: cmd, Permissions: "require_escalated", Justification: "it needs the network"})
	}
	named := make([]Call, 0, len(calls)+len(reply.Calls))
	for _, call := range calls {
		args, err := json.Marshal(call)
		if err != nil {
			panic(err) // a struct of strings always encodes
		}
		named = append(named, Call{Name: "Bash", Args: string(args)})
	}
	named = append(named, reply.Calls...)
	for i, call := range named {
		output = append(output, outputItem{
			ID: fmt.Sprintf("fc-%d-%d", n, i), Type: "function_call", Status: completed,
			CallID: fmt.Sprintf("call-%d-%d", n, i), Name: call.Name, Arguments: call.Args,
		})
	}
	if reply.Text != "" {
		phase := "final_answer"
		if len(named) > 0 {
			phase = "commentary"
		}
		output = append(output, outputItem{
			ID: fmt.Sprintf("msg-%d", n), Type: "message", Role: "assistant", Status: completed, Phase: phase,
			Content: []contentPart{{Type: "output_text", Text: reply.Text, Annotations: []any{}, Logprobs: []any{}}},
		})
	}

	input := 100 * n
	if reply.InputTokens > 0 {
		input = reply.InputTokens
	}

	body := responseBody{ID: fmt.Sprintf("resp-%d", n), Object: "response", Status: completed, Output: output}
	if !reply.NoUsage {
		body.Usage = &usage{InputTokens: input, OutputTokens: 10, TotalTokens: input + 10}
	}

	return body
}

// failWith answers with the reply's HTTP status and a Responses API error.
func failWith(w http.ResponseWriter, reply Reply) {
	body, err := json.Marshal(map[string]any{"error": map[string]any{
		"code": reply.FailCode, "message": "fakellm: " + reply.FailCode, "type": "invalid_request_error",
	}})
	if err != nil {
		panic(err) // a map of strings always encodes
	}
	if reply.FailBody != "" {
		body = []byte(reply.FailBody)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(reply.Fail)
	_, _ = w.Write(body)
}

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
