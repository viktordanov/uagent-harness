package embedded

// Adapted from unreal-agent-runner v0.1.1, harness/llm/clients/openaicodex/credentials.go
// (MIT License, Copyright (c) 2026 Unreal Labs). The package keeps these helpers
// private, and the priority client needs the same credentials.

import (
	"encoding/base64"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/unreallabsai/unreal-agent/harness/llm/clients/openaicodex"
)

type codexCreds struct {
	accessToken string
	accountID   string
}

func codexCredentials(config openaicodex.Config) (codexCreds, error) {
	token, accountID := strings.TrimSpace(config.AccessToken), strings.TrimSpace(config.AccountID)
	if config.AuthFile != "" {
		if token != "" || accountID != "" {
			return codexCreds{}, errors.New("codex AuthFile cannot be combined with AccessToken or AccountID")
		}
		var err error
		token, accountID, err = readCodexAuthFile(config.AuthFile)
		if err != nil {
			return codexCreds{}, err
		}
	}
	if token == "" {
		return codexCreds{}, errors.New("codex access token must be set; use OPENAI_CODEX_ACCESS_TOKEN or a ChatGPT-authenticated Codex auth file")
	}
	if strings.HasPrefix(token, "sk-") || !headerValue(token) {
		return codexCreds{}, errors.New("codex requires a subscription access token, not an API key or invalid header value")
	}
	claimAccount, expires, err := codexTokenClaims(token)
	if err != nil {
		return codexCreds{}, err
	}
	if expires != 0 && time.Now().Unix() >= expires {
		return codexCreds{}, errors.New("codex access token has expired; renew credentials externally")
	}
	if accountID == "" {
		accountID = claimAccount
	} else if claimAccount != "" && accountID != claimAccount {
		return codexCreds{}, errors.New("codex account ID does not match the access token")
	}
	if accountID == "" || !headerValue(accountID) {
		return codexCreds{}, errors.New("codex account ID must be set in OPENAI_CODEX_ACCOUNT_ID, the auth file, or the access token")
	}

	return codexCreds{accessToken: token, accountID: accountID}, nil
}

func readCodexAuthFile(path string) (token, accountID string, err error) {
	file, err := os.Open(path)
	if err != nil {
		return "", "", fmt.Errorf("failed to open the Codex auth file: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", "", fmt.Errorf("failed to inspect the Codex auth file: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return "", "", errors.New("codex auth file must be a regular file with private permissions (chmod 600)")
	}
	const limit = 1 << 20
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return "", "", fmt.Errorf("failed to read the Codex auth file: %w", err)
	}
	var auth struct {
		Mode   string `json:"auth_mode"`
		Tokens struct {
			AccessToken string `json:"access_token"`
			AccountID   string `json:"account_id"`
		} `json:"tokens"`
	}
	if len(data) > limit || json.Unmarshal(data, &auth) != nil {
		return "", "", errors.New("invalid Codex auth file; expected a JSON object with tokens.access_token and tokens.account_id")
	}
	if auth.Mode != "" && auth.Mode != "chatgpt" {
		return "", "", errors.New("codex auth file is not a ChatGPT subscription login")
	}

	return strings.TrimSpace(auth.Tokens.AccessToken), strings.TrimSpace(auth.Tokens.AccountID), nil
}

// codexTokenClaims reads unverified routing and expiry hints; the server
// authenticates the token.
func codexTokenClaims(token string) (accountID string, expires int64, err error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", 0, nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", 0, errors.New("invalid Codex access token claims")
	}
	var claims struct {
		Expires int64 `json:"exp"`
		Auth    struct {
			AccountID string `json:"chatgpt_account_id"`
		} `json:"https://api.openai.com/auth"`
	}
	if json.Unmarshal(payload, &claims) != nil {
		return "", 0, errors.New("invalid Codex access token claims")
	}

	return claims.Auth.AccountID, claims.Expires, nil
}

func headerValue(value string) bool {
	return value != "" && !strings.ContainsFunc(value, func(r rune) bool { return r <= ' ' || r > '~' })
}
