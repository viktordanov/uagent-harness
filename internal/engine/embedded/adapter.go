package embedded

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/unreallabsai/unreal-agent/harness/llm"
)

// switcher is the llm.Adapter the coordinator calls. It applies the live
// model to each request and routes to the priority client when the tier asks
// for it, so /model and /fast apply from the next model request.
type switcher struct {
	build func(priority bool) (Client, error)

	mu       sync.Mutex
	model    string
	priority bool
	clients  map[bool]Client
}

func newSwitcher(model string, priority bool, build func(bool) (Client, error)) (*switcher, error) {
	s := &switcher{build: build, model: model, clients: map[bool]Client{}}
	if err := s.setPriority(priority); err != nil {
		return nil, err
	}

	return s, nil
}

func (s *switcher) Respond(ctx context.Context, req llm.Request, opts llm.RequestOptions) (llm.Response, error) {
	s.mu.Lock()
	client, model := s.clients[s.priority], s.model
	s.mu.Unlock()
	if model != "" {
		req.Model.ID = model
	}

	return client.Respond(ctx, req, opts) //nolint:wrapcheck // the coordinator wraps model errors
}

func (s *switcher) setModel(model string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.model = model
}

// setPriority builds the client for the tier on first use.
func (s *switcher) setPriority(priority bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.clients[priority]; !ok {
		c, err := s.build(priority)
		if err != nil {
			if priority {
				return fmt.Errorf("failed to enable priority processing: %w", err)
			}

			return err
		}
		s.clients[priority] = c
	}
	s.priority = priority

	return nil
}

func (s *switcher) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var errs []error
	for _, c := range s.clients {
		errs = append(errs, c.Close())
	}

	return errors.Join(errs...)
}

// currentModel is the model the next request goes to.
func (s *switcher) currentModel() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.model
}

// direct is the current client without the live model override, for
// one-shot calls that pick their own model; an empty model is the live one.
func (s *switcher) direct() llm.Adapter {
	return adapterFunc(func(ctx context.Context, req llm.Request, opts llm.RequestOptions) (llm.Response, error) {
		s.mu.Lock()
		client, model := s.clients[s.priority], s.model
		s.mu.Unlock()
		if req.Model.ID == "" {
			req.Model.ID = model
		}

		return client.Respond(ctx, req, opts) //nolint:wrapcheck // llmcall wraps model errors
	})
}

type adapterFunc func(context.Context, llm.Request, llm.RequestOptions) (llm.Response, error)

func (f adapterFunc) Respond(ctx context.Context, req llm.Request, opts llm.RequestOptions) (llm.Response, error) {
	return f(ctx, req, opts)
}
