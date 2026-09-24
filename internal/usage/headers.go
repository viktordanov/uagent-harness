package usage

import (
	"encoding/json"
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
)

// ParseHeaders reads the rate-limit headers the backend sends on every
// /responses call, as Codex does (codex-api/src/rate_limits.rs:22-102 and
// 195-272): x-codex-primary-used-percent, -window-minutes, -reset-at, the
// same for secondary, x-codex-credits-*, and one family per extra limit
// (x-<id>-primary-used-percent). It reports false when no family has data.
func ParseHeaders(h http.Header, now time.Time) (Snapshot, bool) {
	s := Snapshot{
		CapturedAt:  now,
		ReachedType: strings.TrimSpace(h.Get("X-Codex-Rate-Limit-Reached-Type")),
		// Codex does not read this header; the backend sent it in the probe.
		Plan: strings.TrimSpace(h.Get("X-Codex-Plan-Type")),
	}
	ids := []string{CodexLimitID}
	for name := range h {
		lower := strings.ToLower(name)
		prefix, ok := strings.CutSuffix(lower, "-primary-used-percent")
		if !ok {
			continue
		}
		if id, ok := strings.CutPrefix(prefix, "x-"); ok && normalizeID(id) != CodexLimitID {
			ids = append(ids, normalizeID(id))
		}
	}
	slices.Sort(ids[1:])
	for _, id := range slices.Compact(ids) {
		l, ok := headerLimit(h, id)
		if ok {
			s.Limits = append(s.Limits, l)
		}
	}
	has, errHas := strconv.ParseBool(h.Get("X-Codex-Credits-Has-Credits"))
	unlimited, errUnl := strconv.ParseBool(h.Get("X-Codex-Credits-Unlimited"))
	if errHas == nil && errUnl == nil {
		s.Credits = &Credits{HasCredits: has, Unlimited: unlimited, Balance: strings.TrimSpace(h.Get("X-Codex-Credits-Balance"))}
	}

	return s, len(s.Limits) > 0 || s.Credits != nil
}

func headerLimit(h http.Header, id string) (Limit, bool) {
	prefix := "x-" + strings.ReplaceAll(id, "_", "-")
	l := Limit{
		ID:        id,
		Name:      strings.TrimSpace(h.Get(prefix + "-limit-name")),
		Primary:   headerWindow(h, prefix+"-primary"),
		Secondary: headerWindow(h, prefix+"-secondary"),
	}

	return l, l.Primary != nil || l.Secondary != nil
}

// headerWindow drops a window whose values are all zero, as Codex does.
func headerWindow(h http.Header, prefix string) *Window {
	used, err := strconv.ParseFloat(strings.TrimSpace(h.Get(prefix+"-used-percent")), 64)
	if err != nil || math.IsNaN(used) || math.IsInf(used, 0) {
		return nil
	}
	w := &Window{UsedPercent: used}
	w.Minutes, _ = strconv.ParseInt(strings.TrimSpace(h.Get(prefix+"-window-minutes")), 10, 64)
	if at, err := strconv.ParseInt(strings.TrimSpace(h.Get(prefix+"-reset-at")), 10, 64); err == nil {
		w.ResetsAt = time.Unix(at, 0)
	}
	if used == 0 && w.Minutes == 0 && w.ResetsAt.IsZero() {
		return nil
	}

	return w
}

func normalizeID(id string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(id)), "-", "_")
}

// LimitReached is the body of a 429 whose error type is
// "usage_limit_reached" (codex-api/src/api_bridge.rs:149-174 and 294-305).
type LimitReached struct {
	Plan string
	// ResetsAt is zero when the backend sent no time.
	ResetsAt time.Time
}

// ParseLimitReached reads a 429 body; it reports false for any other error.
func ParseLimitReached(body []byte) (LimitReached, bool) {
	var e struct {
		Error struct {
			Type     string `json:"type"`
			PlanType string `json:"plan_type"`
			ResetsAt int64  `json:"resets_at"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &e) != nil || e.Error.Type != "usage_limit_reached" {
		return LimitReached{}, false
	}
	r := LimitReached{Plan: e.Error.PlanType}
	if e.Error.ResetsAt > 0 {
		r.ResetsAt = time.Unix(e.Error.ResetsAt, 0)
	}

	return r, true
}
