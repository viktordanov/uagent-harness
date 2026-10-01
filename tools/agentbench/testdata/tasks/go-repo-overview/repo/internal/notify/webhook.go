package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// WebhookSender posts the event as JSON, signed with the subscriber's
// secret in X-Shipd-Signature.
type WebhookSender struct {
	client *http.Client
}

// NewWebhookSender returns a sender using client.
func NewWebhookSender(client *http.Client) *WebhookSender {
	return &WebhookSender{client: client}
}

// Send posts m once. A 4xx answer is a PermanentError.
func (s *WebhookSender) Send(ctx context.Context, m Message) error {
	body, err := json.Marshal(m.Event)
	if err != nil {
		return &PermanentError{err}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.URL, bytes.NewReader(body))
	if err != nil {
		return &PermanentError{err}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Shipd-Signature", sign(m.Secret, body))
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode >= 500:
		return fmt.Errorf("webhook %s: %s", m.URL, resp.Status)
	case resp.StatusCode >= 400:
		return &PermanentError{fmt.Errorf("webhook %s: %s", m.URL, resp.Status)}
	}

	return nil
}
