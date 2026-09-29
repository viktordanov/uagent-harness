package app

import (
	"fmt"

	"github.com/viktordanov/uagent-harness/internal/config"
)

// Codex's web_search values that uah offers (docs/design/web-search.md).
const (
	WebSearchLive     = "live"
	WebSearchDisabled = "disabled"
)

// pickWebSearch checks web_search: live by default, as Codex turns search
// on by default. Codex's cached and indexed modes need tool options the
// runner does not send, so they are errors rather than a silent live.
func pickWebSearch(cfg config.Config) (string, error) {
	switch cfg.WebSearch {
	case "", WebSearchLive:
		return WebSearchLive, nil
	case WebSearchDisabled:
		return WebSearchDisabled, nil
	case "cached", "indexed":
		return "", usage(fmt.Errorf("web_search = %q is not available: the runner sends web search without Codex's access options, which is live search; use \"live\" or \"disabled\"", cfg.WebSearch))
	}

	return "", usage(fmt.Errorf("invalid web_search %q (want live or disabled)", cfg.WebSearch))
}
