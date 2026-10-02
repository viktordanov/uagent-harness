package codexauth

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

// Transport returns a transport that sends each request with the login's
// credentials as they are then (Creds): Authorization and
// ChatGPT-Account-ID. When the backend answers 401, it renews them (Renew)
// and sends the request once more, as Codex does for ChatGPT logins
// (UnauthorizedRecovery, manager.rs:1830-2003). The environment's token is
// sent as it is. A login that expired and cannot be refreshed answers 401,
// which the runner's client does not retry; a refresh that failed otherwise
// is an error, which it retries.
func (l *Login) Transport(base http.RoundTripper) http.RoundTripper {
	return transport{login: l, base: base}
}

type transport struct {
	login *Login
	base  http.RoundTripper
}

func (t transport) RoundTrip(req *http.Request) (*http.Response, error) {
	creds, err := t.login.Creds(req.Context())
	if errors.Is(err, ErrLoginExpired) {
		return unauthorized(req), nil
	}
	if err != nil {
		return nil, err
	}
	resp, err := t.base.RoundTrip(withCreds(req, creds))
	if err != nil || resp.StatusCode != http.StatusUnauthorized || !t.login.FromFile() ||
		(req.Body != nil && req.Body != http.NoBody && req.GetBody == nil) {
		return resp, err // a transport's errors pass through
	}
	renewed, err := t.login.Renew(req.Context(), creds)
	switch {
	case errors.Is(err, ErrLoginExpired):
		return resp, nil
	case err != nil:
		// The runner's client retries a failed attempt, which renews
		// again; it gives up on a 401.
		discard(resp)

		return nil, err
	}
	retry := withCreds(req, renewed)
	if req.GetBody != nil {
		body, err := req.GetBody()
		if err != nil {
			return resp, nil // the caller reads the 401
		}
		retry.Body = body
	}
	discard(resp)

	return t.base.RoundTrip(retry)
}

func discard(resp *http.Response) {
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	_ = resp.Body.Close()
}

// unauthorized stands for the backend's 401 when the login expired and
// cannot be refreshed, so the caller gives up as it does on a 401 instead of
// retrying.
func unauthorized(req *http.Request) *http.Response {
	const body = `{"error":{"message":"the refresh token was refused"}}`

	return &http.Response{
		Status: "401 Unauthorized", StatusCode: http.StatusUnauthorized, Proto: "HTTP/1.1", ProtoMajor: 1, ProtoMinor: 1,
		Header:        http.Header{"Content-Type": {"application/json"}},
		Body:          io.NopCloser(strings.NewReader(body)),
		ContentLength: int64(len(body)), Request: req,
	}
}

// withCreds is a copy of req with the credentials' headers.
func withCreds(req *http.Request, creds Creds) *http.Request {
	out := req.Clone(req.Context())
	out.Header.Set("Authorization", "Bearer "+creds.AccessToken)
	out.Header.Set("Chatgpt-Account-Id", creds.AccountID)

	return out
}

// Status describes the login for `uah doctor`, without its tokens.
type Status struct {
	// File is the auth file; empty for OPENAI_CODEX_ACCESS_TOKEN.
	File string
	// Expires is when the access token expires (zero: unknown), and
	// LastRefresh when Codex or uah last refreshed it (zero: unknown).
	Expires, LastRefresh time.Time
	// Refreshable is whether the file has a refresh token uah has not seen
	// refused.
	Refreshable bool
}

// Status reads the login without refreshing it.
func (l *Login) Status() (Status, error) {
	var s Status
	token := l.config.AccessToken
	if l.file != nil {
		auth, err := l.file.read()
		if err != nil {
			return Status{}, err
		}
		l.file.refreshing.Lock()
		failed := l.file.failed
		l.file.refreshing.Unlock()
		token, s.File, s.LastRefresh = auth.token, l.file.path, auth.lastRefresh
		s.Refreshable = auth.refreshToken != "" && auth.refreshToken != failed
	}
	if _, expires, err := codexTokenClaims(token); err == nil && expires != 0 {
		s.Expires = time.Unix(expires, 0)
	}

	return s, nil
}
