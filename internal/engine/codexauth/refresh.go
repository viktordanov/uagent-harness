package codexauth

// The refresh request and the classification of its failures are adapted
// from OpenAI Codex rust-v0.156.1 (Apache License 2.0, Copyright 2025
// OpenAI; see THIRD_PARTY_NOTICES.md): codex-rs/login/src/auth/manager.rs
// (request_chatgpt_token_refresh, classify_refresh_token_failure, CLIENT_ID,
// REFRESH_TOKEN_URL) and codex-rs/login/src/oauth/client.rs and error.rs
// (the JSON refresh grant and the error code it reads).

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// tokenURL and clientID are Codex's (manager.rs:212 and 1698).
	tokenURL = "https://auth.openai.com/oauth/token"
	clientID = "app_EMoamEEZ73f0CkXaXp7hrann"
	// TokenURLEnv overrides the token endpoint, as in Codex
	// (CODEX_REFRESH_TOKEN_URL_OVERRIDE, manager.rs:214). uah accepts only a
	// loopback endpoint, for tests, so the refresh token goes nowhere else.
	TokenURLEnv = "CODEX_REFRESH_TOKEN_URL_OVERRIDE"
	// refreshToken is the grant type and the field of the token.
	refreshToken = "refresh_token"
	// refreshTimeout bounds one refresh.
	refreshTimeout = 30 * time.Second
)

// ErrLoginExpired means the ChatGPT login can no longer be refreshed: the
// refresh token expired, was used already, or was revoked. Codex asks the
// user to sign in again in that case. The message is a sentence the user
// reads as it is.
var ErrLoginExpired = errors.New("Your ChatGPT login expired; run `codex login`") //nolint:staticcheck,revive // shown as is

// tokens are the token endpoint's answer (manager.rs RefreshResponse); each
// may be missing.
type tokens struct {
	IDToken      string `json:"id_token"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

// refresher asks the token endpoint for new tokens.
type refresher struct {
	url    string
	client *http.Client
}

func newRefresher(override string) (refresher, error) {
	endpoint := tokenURL
	if override = strings.TrimSpace(override); override != "" {
		if !loopback(override) {
			return refresher{}, errors.New(TokenURLEnv + " must be an explicit loopback IP endpoint")
		}
		endpoint = override
	}

	return refresher{url: endpoint, client: &http.Client{
		Timeout:       refreshTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}, nil
}

func loopback(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.User != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return false
	}
	ip := net.ParseIP(parsed.Hostname())

	return ip != nil && ip.IsLoopback()
}

// refresh sends Codex's refresh grant: a JSON POST of client_id,
// grant_type, and refresh_token (oauth/client.rs refresh and exchange).
// Errors never include a token or the endpoint's answer.
func (r refresher) refresh(ctx context.Context, token string) (tokens, error) {
	body, err := json.Marshal(map[string]string{"client_id": clientID, "grant_type": refreshToken, refreshToken: token})
	if err != nil {
		return tokens{}, fmt.Errorf("failed to encode the refresh request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.url, bytes.NewReader(body))
	if err != nil {
		return tokens{}, fmt.Errorf("failed to build the refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := r.client.Do(req)
	if err != nil {
		// Not wrapped: a request that fails here is not a lost
		// connection to the model, which the runner would retry.
		return tokens{}, fmt.Errorf("failed to refresh the ChatGPT login: %v", err) //nolint:errorlint // see above
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return tokens{}, fmt.Errorf("failed to refresh the ChatGPT login: %v", err) //nolint:errorlint // as above
	}
	if resp.StatusCode != http.StatusOK {
		return tokens{}, refused(resp.StatusCode, data)
	}
	var t tokens
	if json.Unmarshal(data, &t) != nil || t.AccessToken == "" {
		return tokens{}, errors.New("failed to refresh the ChatGPT login: the token endpoint's answer has no access token")
	}

	return t, nil
}

// refused classifies a refusal as Codex does (manager.rs:1636-1690): a 401,
// invalid_grant on a 400, and the codes refresh_token_expired,
// refresh_token_reused, and refresh_token_invalidated are permanent, so the
// user must sign in again. Anything else may pass.
func refused(status int, body []byte) error {
	code := strings.ToLower(errorCode(body))
	switch {
	case status == http.StatusUnauthorized,
		status == http.StatusBadRequest && code == "invalid_grant",
		code == "refresh_token_expired", code == "refresh_token_reused", code == "refresh_token_invalidated":
		return ErrLoginExpired
	}

	return fmt.Errorf("failed to refresh the ChatGPT login: the token endpoint answered %d %s", status, http.StatusText(status))
}

// errorCode is the answer's error code: "error" as a string, else
// "error.code", else "code" (oauth/error.rs TokenErrorDetail::parse).
func errorCode(body []byte) string {
	var answer map[string]any
	if json.Unmarshal(body, &answer) != nil {
		return ""
	}
	if s, ok := answer["error"].(string); ok && strings.TrimSpace(s) != "" {
		return s
	}
	if inner, ok := answer["error"].(map[string]any); ok {
		if s, ok := inner["code"].(string); ok && strings.TrimSpace(s) != "" {
			return s
		}
	}
	s, _ := answer["code"].(string)

	return s
}
