package agents

import (
	"encoding/json"
	"strings"
)

// maxCause is how long a failure's cause may be in a status.
const maxCause = 500

// readable turns a run's error into one line for the parent: the message a
// provider put in its JSON error body when there is one, such as
// {"detail":"The 'x' model is not supported ..."} or
// {"error":{"message":"..."}}, else the error itself.
func readable(msg string) string {
	for i := strings.IndexByte(msg, '{'); i >= 0; {
		if m := providerMessage(msg[i:]); m != "" {
			msg = m

			break
		}
		next := strings.IndexByte(msg[i+1:], '{')
		if next < 0 {
			break
		}
		i += 1 + next
	}
	msg = strings.Join(strings.Fields(msg), " ")
	if len(msg) > maxCause {
		msg = msg[:maxCause-1] + "…"
	}

	return msg
}

// providerMessage reads the message of a JSON error body at the start of s.
func providerMessage(s string) string {
	var body struct {
		Detail  any    `json:"detail"`
		Message string `json:"message"`
		Error   any    `json:"error"`
	}
	if json.NewDecoder(strings.NewReader(s)).Decode(&body) != nil {
		return ""
	}
	if d, ok := body.Detail.(string); ok && d != "" {
		return d
	}
	switch e := body.Error.(type) {
	case string:
		return e
	case map[string]any:
		if m, ok := e["message"].(string); ok {
			return m
		}
	}

	return body.Message
}
