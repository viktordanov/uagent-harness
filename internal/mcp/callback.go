package mcp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
)

// callback is the loopback listener the browser returns to after the user
// authorizes, as Codex's: 127.0.0.1 on the configured port or one the OS
// picks, path /callback.
type callback struct {
	redirect string
	server   *http.Server
	results  chan callbackResult
}

type callbackResult struct {
	res *auth.AuthorizationResult
	err error
}

// listenCallback binds the listener. The server's oauth table overrides
// the top-level port and URL.
func listenCallback(ctx context.Context, server *OAuthConfig, s OAuthSettings) (*callback, error) {
	port, redirect := s.CallbackPort, s.CallbackURL
	if server != nil && server.CallbackPort != nil {
		port = *server.CallbackPort
	}
	if server != nil && server.CallbackURL != "" {
		redirect = server.CallbackURL
	}
	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return nil, fmt.Errorf("failed to listen for the OAuth callback: %w", err)
	}
	path := "/callback"
	if redirect == "" {
		redirect = "http://" + ln.Addr().String() + path
	} else if u, err := url.Parse(redirect); err == nil && u.Path != "" {
		path = u.Path
	}
	cb := &callback{redirect: redirect, results: make(chan callbackResult, 1)}
	mux := http.NewServeMux()
	mux.HandleFunc(path, cb.handle)
	cb.server = &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = cb.server.Serve(ln) }()

	return cb, nil
}

// handle takes the authorization server's redirect.
func (cb *callback) handle(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var res callbackResult
	switch {
	case q.Get("error") != "":
		res.err = fmt.Errorf("the authorization server refused: %s %s", q.Get("error"), q.Get("error_description"))
	case q.Get("code") == "":
		http.Error(w, "Invalid OAuth callback", http.StatusBadRequest)

		return
	default:
		res.res = &auth.AuthorizationResult{Code: q.Get("code"), State: q.Get("state"), Iss: q.Get("iss")}
	}
	select {
	case cb.results <- res:
	default: // a second callback: the first one counts
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if res.err != nil {
		_, _ = io.WriteString(w, "Authentication failed. You may close this window.")

		return
	}
	_, _ = io.WriteString(w, "Authentication complete. You may close this window.")
}

// fetch shows the authorization URL, opens the browser, and waits for the
// callback.
func (cb *callback) fetch(ctx context.Context, name, authURL string, opts LoginOptions) (*auth.AuthorizationResult, error) {
	fmt.Fprintf(opts.Out, "Authorize `%s` by opening this URL in your browser:\n%s\n\n", name, authURL)
	if opts.OpenBrowser != nil {
		if err := opts.OpenBrowser(authURL); err != nil {
			fmt.Fprintln(opts.Out, "(Browser launch failed; please copy the URL above manually.)")
		}
	}
	timer := time.NewTimer(opts.Timeout)
	defer timer.Stop()
	select {
	case r := <-cb.results:
		return r.res, r.err
	case <-timer.C:
		return nil, errors.New("timed out waiting for OAuth callback")
	case <-ctx.Done():
		return nil, fmt.Errorf("the login was canceled: %w", ctx.Err())
	}
}

func (cb *callback) close() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = cb.server.Shutdown(ctx)
}

// checkCallbackURL accepts an absolute http(s) URL.
func checkCallbackURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return err //nolint:wrapcheck // the caller names the key
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("%q is not an http(s) URL", raw)
	}

	return nil
}

// resourceURL replaces the authorization URL's RFC 8707 resource with
// oauth_resource, when set.
func resourceURL(authURL, resource string) string {
	if resource == "" {
		return authURL
	}
	u, err := url.Parse(authURL)
	if err != nil {
		return authURL
	}
	q := u.Query()
	q.Set("resource", resource)
	u.RawQuery = q.Encode()

	return u.String()
}

// withResource returns a client that sends oauth_resource as the resource
// of token requests, where the SDK sends the server's own.
func withResource(client *http.Client, resource string) *http.Client {
	if resource == "" {
		return client
	}
	base := client.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	c := *client
	c.Transport = resourceTransport{base: base, resource: resource}

	return &c
}

type resourceTransport struct {
	base     http.RoundTripper
	resource string
}

func (t resourceTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Method != http.MethodPost || r.Body == nil || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
		return t.base.RoundTrip(r) //nolint:wrapcheck // a transport passes errors through
	}
	body, err := io.ReadAll(r.Body)
	_ = r.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("failed to read the token request: %w", err)
	}
	form, err := url.ParseQuery(string(body))
	if err == nil && form.Has("resource") {
		form.Set("resource", t.resource)
		body = []byte(form.Encode())
	}
	r = r.Clone(r.Context())
	r.Body = io.NopCloser(bytes.NewReader(body))
	r.ContentLength = int64(len(body))

	return t.base.RoundTrip(r) //nolint:wrapcheck // a transport passes errors through
}
