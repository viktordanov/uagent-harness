package embedded

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/viktordanov/uah-core/harness/llm"
	"github.com/viktordanov/uah-core/harness/llm/responsesapi"

	"github.com/viktordanov/uah/internal/engine"
)

// Effort updates' fallback: a backend that does not take configuration_update
// items rejects a request that carries them as an invalid request. The
// switcher then sends the request once more without them, at the effort
// they set (the request's own effort, as without updates: the user's, or
// adaptive effort's choice), and turns effort updates off for the session:
// for the rest of the run, and through sessions/<id>.effortupdates.json
// for its later runs and its forks. The retry carries no update, so it
// cannot fall back again.
//
// Which failures count (rejectionOf): an error that names the item (its
// type in the code, message, or param) on a 4xx or a failed stream turns
// updates off at once. An invalid request (400 or 422) whose error has no
// code or a schema code and points at the input, or at nothing, may be the
// items: the retry without them decides, and only its success turns
// updates off; when it fails too, the first error stands. Any other
// failure, such as a context too long, a quota, or an invalid tool, fails
// the request as before, with no retry.

// rejection is what a failed request's error says of its effort updates.
type rejection int

const (
	// notRejected: the error is not about the updates.
	notRejected rejection = iota
	// rejected: the error names the configuration_update item.
	rejected
	// maybeRejected: an invalid input that the items may have caused.
	maybeRejected
)

// schemaCodes are the error codes of an input the backend cannot parse,
// "" for an error without a code, such as the ChatGPT backend's
// {"detail":"..."}.
var schemaCodes = []string{"", "invalid_value", "invalid_type", "unknown_parameter", "unsupported_value", "unsupported_parameter", "invalid_request_error"}

// rejectionOf classifies the error of a request that carried effort
// updates.
func rejectionOf(err error) rejection {
	e, ok := errors.AsType[*responsesapi.APIError](err)
	if !ok {
		return notRejected
	}
	client := e.StatusCode >= http.StatusBadRequest && e.StatusCode < http.StatusInternalServerError
	text := strings.ToLower(strings.Join([]string{e.Code, e.Message, e.Param}, " "))
	if (client || e.StatusCode == http.StatusOK) && (strings.Contains(text, string(llm.ItemConfigurationUpdate)) || strings.Contains(text, "configuration update")) {
		return rejected
	}
	invalid := e.StatusCode == http.StatusBadRequest || e.StatusCode == http.StatusUnprocessableEntity
	if invalid && slices.Contains(schemaCodes, e.Code) && (e.Param == "" || strings.HasPrefix(e.Param, "input")) {
		return maybeRejected
	}

	return notRejected
}

// noUpdatesKey marks the retry of a rejected request, which goes without
// effort updates.
type noUpdatesKey struct{}

// fallBack handles the failure err of req, which carried effort updates:
// when the backend rejected them, it sends req again without them and
// turns them off (see above); otherwise it returns err.
func (s *switcher) fallBack(ctx context.Context, req llm.Request, opts llm.RequestOptions, err error) (llm.Response, error) {
	r := rejectionOf(err)
	if r == notRejected {
		return llm.Response{}, err
	}
	if r == rejected {
		s.rejectUpdates(err)
	}
	resp, _, retryErr := s.respond(context.WithValue(ctx, noUpdatesKey{}, true), req, opts)
	if r == maybeRejected {
		if retryErr != nil {
			s.diagUpdates(time.Now(), "kept", err.Error(), retryErr.Error())

			return llm.Response{}, err
		}
		s.rejectUpdates(err)
	}

	return resp, retryErr
}

// rejectUpdates turns the session's effort updates off, once: it logs the
// error, saves the choice (offUpdates), and reports EffortUpdatesOff.
func (s *switcher) rejectUpdates(err error) {
	s.mu.Lock()
	if s.rejected {
		s.mu.Unlock()

		return
	}
	s.rejected, s.update = true, nil
	save := s.offUpdates
	s.mu.Unlock()
	e := engine.EffortUpdatesOff{At: time.Now(), Err: err.Error()}
	s.diagUpdates(e.At, "off", e.Err, "")
	if save != nil {
		if serr := save(e); serr != nil {
			s.diagUpdates(time.Now(), "unsaved", serr.Error(), "")
		}
	}
	if s.stream != nil {
		s.stream(e)
	}
}

// effortUpdatesDiag is the stderr.log line of a rejection: updates "off",
// or "kept" when the retry without them failed too, or "unsaved" when the
// session's file could not be written.
type effortUpdatesDiag struct {
	Diag       string    `json:"diag"`
	At         time.Time `json:"at"`
	Result     string    `json:"result"`
	Error      string    `json:"error"`
	RetryError string    `json:"retry_error,omitempty"`
}

// diagUpdates writes an effort_updates line to the diagnostics.
func (s *switcher) diagUpdates(at time.Time, result, err, retryErr string) {
	d := effortUpdatesDiag{Diag: "effort_updates", At: at, Result: result, Error: err, RetryError: retryErr}
	if line, merr := json.Marshal(d); merr == nil && s.diag != nil {
		_, _ = s.diag.Write(append(line, '\n'))
	}
}

// effortUpdatesRecord is sessions/<id>.effortupdates.json: the session's
// effort updates have been off since At, when the backend rejected them
// with Error.
type effortUpdatesRecord struct {
	At    time.Time `json:"at"`
	Error string    `json:"error"`
}

func effortUpdatesPath(dir, sessionID string) string {
	return filepath.Join(dir, sessionID+".effortupdates.json")
}

// updatesRejected reports whether the session's effort updates are off.
func updatesRejected(dir, sessionID string) (bool, error) {
	_, err := os.Stat(effortUpdatesPath(dir, sessionID))
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to read the session's effort updates: %w", err)
	}

	return true, nil
}

// saveUpdatesRejected turns the session's effort updates off for its later
// runs.
func saveUpdatesRejected(dir, sessionID string, e engine.EffortUpdatesOff) error {
	data, err := json.Marshal(effortUpdatesRecord{At: e.At, Error: e.Err})
	if err != nil {
		return fmt.Errorf("failed to encode the session's effort updates: %w", err)
	}
	if err := os.WriteFile(effortUpdatesPath(dir, sessionID), append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("failed to save the session's effort updates: %w", err)
	}

	return nil
}

// copyUpdatesRejected gives a fork its parent's file, so it does not send
// the updates the backend rejected.
func copyUpdatesRejected(dir, parentID, childID string) error {
	data, err := os.ReadFile(effortUpdatesPath(dir, parentID))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err == nil {
		err = os.WriteFile(effortUpdatesPath(dir, childID), data, 0o600)
	}
	if err != nil {
		return fmt.Errorf("failed to copy the session's effort updates: %w", err)
	}

	return nil
}
