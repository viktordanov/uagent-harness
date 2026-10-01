package codexauth

import (
	"bytes"
	"context"
	"errors"
	"time"

	"github.com/viktordanov/unreal-agent/harness/llm/clients/openaicodex"
)

// Provider is the provider whose credentials this package serves.
const Provider = "openai-codex"

const (
	// refreshWindow and refreshInterval are when Codex refreshes a token
	// before it is used (should_refresh_proactively, manager.rs:203-204 and
	// 2955-2977): when it expires within five minutes, or, for a token
	// without an expiry, when the file was last refreshed over eight days
	// ago.
	refreshWindow   = 5 * time.Minute
	refreshInterval = 8 * 24 * time.Hour
	// runWindow is how soon a token may expire when a run starts: uagent's
	// preflight (v0.4.4, harness/preflight.go:18 and 145-157) warns within
	// an hour and blocks an expired token.
	runWindow = time.Hour
)

// Login is the ChatGPT login the environment selects: OPENAI_CODEX_ACCESS_TOKEN,
// which uah never refreshes, or Codex's auth file (OPENAI_CODEX_AUTH_FILE,
// else $CODEX_HOME/auth.json), which it refreshes as Codex does and shares
// with every other Login in the process that names the same file.
type Login struct {
	config  openaicodex.Config
	file    *file // nil for the environment's token
	refresh refresher
}

// Open returns the login the environment selects, as the runner's client
// finds it (openaicodex.EnvironmentConfig).
func Open(getenv func(string) string) (*Login, error) {
	config, err := openaicodex.EnvironmentConfig(getenv)
	if err != nil {
		return nil, err //nolint:wrapcheck // the caller wraps it
	}
	r, err := newRefresher(getenv(TokenURLEnv))
	if err != nil {
		return nil, err
	}
	l := &Login{config: config, refresh: r}
	if config.AuthFile != "" {
		if config.AccessToken != "" || config.AccountID != "" {
			return nil, errors.New("codex AuthFile cannot be combined with AccessToken or AccountID")
		}
		l.file = fileFor(config.AuthFile)
	}

	return l, nil
}

// FromFile reports whether the login is Codex's auth file, which uah
// refreshes.
func (l *Login) FromFile() bool { return l.file != nil }

// Check reads and checks the credentials without refreshing them, as a
// client does when it is built. A token that expired passes when the file
// has a refresh token, since the first request refreshes it.
func (l *Login) Check() (Creds, error) {
	if l.file == nil {
		return Load(l.config)
	}
	auth, err := l.file.read()
	if err != nil {
		return Creds{}, err
	}
	creds, expires, err := check(auth.token, auth.accountID)
	if err != nil {
		return Creds{}, err
	}
	if expired(expires, time.Now()) && auth.refreshToken == "" {
		return Creds{}, ErrLoginExpired
	}

	return creds, nil
}

// Creds returns the credentials for a request. It reads the auth file again
// when it changed, and refreshes a token Codex would refresh before using
// it (refreshWindow). When that refresh fails and the token still works, it
// returns the token, as Codex does.
func (l *Login) Creds(ctx context.Context) (Creds, error) {
	if l.file == nil {
		return Load(l.config)
	}
	auth, err := l.file.read()
	if err != nil {
		return Creds{}, err
	}
	due := func(a fileAuth) bool { return expiresWithin(a, refreshWindow) || staleWithoutExpiry(a) }
	if !due(auth) {
		return usable(auth)
	}
	renewed, err := l.renew(ctx, due)
	if err != nil {
		if creds, usableErr := usable(auth); usableErr == nil {
			return creds, nil
		}

		return Creds{}, err
	}

	return usable(renewed)
}

// Renew returns credentials to replace ones the backend rejected with a
// 401: the file's, when another writer such as Codex already replaced the
// rejected token, else refreshed ones, as Codex's UnauthorizedRecovery does
// (manager.rs:1830-2003). The environment's token is never refreshed.
func (l *Login) Renew(ctx context.Context, rejected Creds) (Creds, error) {
	if l.file == nil {
		return Creds{}, errors.New("codex credentials rejected; OPENAI_CODEX_ACCESS_TOKEN is not refreshed, so set a new one")
	}
	renewed, err := l.renew(ctx, func(a fileAuth) bool {
		return a.token == rejected.AccessToken || expiresWithin(a, 0)
	})
	if err != nil {
		return Creds{}, err
	}

	return usable(renewed)
}

// BeforeRun refreshes the provider's login before a run starts when its
// token expires within runWindow, so uagent's preflight never blocks a
// token that can be refreshed. It fails only when the token expired and
// cannot be refreshed; preflight reports every other problem.
func BeforeRun(ctx context.Context, provider string, getenv func(string) string) error {
	if provider != Provider {
		return nil
	}
	l, err := Open(getenv)
	if err != nil || l.file == nil {
		return nil //nolint:nilerr // preflight reports it
	}
	auth, err := l.file.read()
	if err != nil || !expiresWithin(auth, runWindow) && !staleWithoutExpiry(auth) {
		return nil //nolint:nilerr // preflight reports it
	}
	_, err = l.renew(ctx, func(a fileAuth) bool { return expiresWithin(a, runWindow) || staleWithoutExpiry(a) })
	if err != nil && expiresWithin(auth, 0) {
		return err
	}

	return nil
}

// renew refreshes the file's token, once at a time in the process and,
// through the lock, among uah processes. The file is read again under the
// lock: when due says it no longer needs a refresh, another writer (a
// concurrent request, another uah, or Codex) refreshed it, and its tokens
// are used, as Codex's guarded reload does (refresh_token, manager.rs:2799).
// The file is read once more before the write, and a writer that refreshed
// it meanwhile wins.
func (l *Login) renew(ctx context.Context, due func(fileAuth) bool) (fileAuth, error) {
	f := l.file
	f.refreshing.Lock()
	defer f.refreshing.Unlock()
	unlock, err := f.lock(ctx)
	if err != nil {
		return fileAuth{}, err
	}
	defer unlock()
	cur, err := f.read()
	if err != nil || !due(cur) {
		return cur, err
	}
	if cur.refreshToken == "" || cur.refreshToken == f.failed {
		return fileAuth{}, ErrLoginExpired
	}
	// A refresh token works once, so a canceled request must not drop the
	// answer: the refresh and the write finish on their own.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), refreshTimeout)
	defer cancel()
	t, refreshErr := l.refresh.refresh(ctx, cur.refreshToken)
	latest, err := f.read()
	if err == nil && !bytes.Equal(latest.raw, cur.raw) && !due(latest) {
		return latest, nil
	}
	if refreshErr != nil {
		if errors.Is(refreshErr, ErrLoginExpired) {
			f.failed = cur.refreshToken
		}

		return fileAuth{}, refreshErr
	}
	if err != nil {
		return fileAuth{}, err
	}

	return f.write(latest.raw, t, time.Now())
}

// usable is the file's credentials when the token has not expired.
func usable(a fileAuth) (Creds, error) {
	creds, expires, err := check(a.token, a.accountID)
	if err != nil {
		return Creds{}, err
	}
	if expired(expires, time.Now()) {
		return Creds{}, ErrLoginExpired
	}

	return creds, nil
}

// expiresWithin reports whether the file's token expires within d. A token
// without an expiry never does.
func expiresWithin(a fileAuth, d time.Duration) bool {
	_, expires, err := codexTokenClaims(a.token)

	return err == nil && expires != 0 && !time.Now().Add(d).Before(time.Unix(expires, 0))
}

// staleWithoutExpiry reports whether a token without an expiry was last
// refreshed over refreshInterval ago.
func staleWithoutExpiry(a fileAuth) bool {
	_, expires, err := codexTokenClaims(a.token)

	return err == nil && expires == 0 && !a.lastRefresh.IsZero() && time.Since(a.lastRefresh) > refreshInterval
}
