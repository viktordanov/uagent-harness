package compaction

// DefaultContextWindow is the window of a model the table does not know, as
// Codex assumes for unknown models.
const DefaultContextWindow = 272_000

// DefaultAutoPercent is Codex's automatic compaction limit: 90% of the window.
const DefaultAutoPercent = 90

// baselineTokens is what Codex's meter treats as always in use (the system
// prompt and tools), so a fresh session shows 100% left.
const baselineTokens = 12_000

// WindowLookup finds a model's context window in a model catalog: the
// provider's list, cached, or the bundled one (internal/models' Manager.Window).
// ok is false when the catalog does not know the model.
type WindowLookup func(model string) (window int64, ok bool)

// ContextWindow is the model's context window in tokens: override
// (model_context_window) when it is positive, else the catalog's value
// through lookup (nil: none), else DefaultContextWindow. Everything that
// needs a window calls this; the caller passes the catalog it has.
func ContextWindow(model string, override int64, lookup WindowLookup) int64 {
	if override > 0 {
		return override
	}
	if lookup != nil {
		if w, ok := lookup(model); ok {
			return w
		}
	}

	return DefaultContextWindow
}

// AutoLimit is the tokens in use at which automatic compaction starts; 0
// means never.
func AutoLimit(window int64, percent int) int64 {
	if percent <= 0 || window <= 0 {
		return 0
	}

	return window * int64(min(percent, 100)) / 100
}

// PercentLeft is Codex's "N% context left": the share of the window after
// the baseline that the tokens in use leave free, rounded.
func PercentLeft(used, window int64) int {
	if window <= baselineTokens {
		return 0
	}
	effective := window - baselineTokens
	remaining := max(effective-max(used-baselineTokens, 0), 0)

	return int((remaining*100 + effective/2) / effective)
}
