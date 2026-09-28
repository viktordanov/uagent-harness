package app

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/engine/codexauth"
)

// codexLogin describes the openai-codex login in Codex's auth file for `uah
// doctor`, without its tokens: the file, the token's expiry, when it was last
// refreshed, and whether uah can refresh it. It is empty for another
// provider or OPENAI_CODEX_ACCESS_TOKEN, and refreshable reports whether the
// file has a refresh token, so an expired token does not block a run.
func codexLogin(provider string, getenv func(string) string, now time.Time) (detail string, refreshable bool) {
	if provider != CodexProvider {
		return "", false
	}
	login, err := codexauth.Open(getenv)
	if err != nil || !login.FromFile() {
		return "", false
	}
	s, err := login.Status()
	if err != nil {
		return "", false
	}
	parts := []string{"ChatGPT login in " + s.File}
	switch left := s.Expires.Sub(now); {
	case s.Expires.IsZero():
	case left > 0:
		parts = append(parts, "access token expires in "+span(left))
	default:
		parts = append(parts, "access token expired "+span(-left)+" ago")
	}
	if !s.LastRefresh.IsZero() {
		parts = append(parts, "last refreshed "+span(now.Sub(s.LastRefresh))+" ago")
	}
	if s.Refreshable {
		parts = append(parts, "uah refreshes it with the refresh token, as Codex does")
	} else {
		parts = append(parts, "no refresh token uah can use")
	}

	return strings.Join(parts, "; "), s.Refreshable
}

// withoutExpiry drops preflight's expiry findings, for a token uah refreshes
// before the run.
func withoutExpiry(findings []core.Finding) []core.Finding {
	return slices.DeleteFunc(findings, func(f core.Finding) bool {
		return f.Code == core.FindingAuthExpired || f.Code == core.FindingAuthExpiring
	})
}

// span is a rough duration: days, hours, or minutes.
func span(d time.Duration) string {
	switch {
	case d >= 48*time.Hour:
		return fmt.Sprintf("%d days", int(d/(24*time.Hour)))
	case d >= 2*time.Hour:
		return fmt.Sprintf("%d hours", int(d/time.Hour))
	default:
		return fmt.Sprintf("%d minutes", int(d/time.Minute))
	}
}
