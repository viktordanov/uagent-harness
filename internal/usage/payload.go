package usage

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// payload is the body of GET /wham/usage, as Codex rust-v0.156.1 reads it
// (codex-backend-openapi-models/src/models/rate_limit_status_payload.rs and
// backend-client/src/types.rs:56-67). Unknown fields are ignored, and every
// field but plan_type can be null.
type payload struct {
	PlanType             string            `json:"plan_type"`
	RateLimit            *statusDetails    `json:"rate_limit"`
	Credits              *creditDetails    `json:"credits"`
	AdditionalRateLimits []additionalLimit `json:"additional_rate_limits"`
	RateLimitReachedType *struct {
		Type string `json:"type"`
	} `json:"rate_limit_reached_type"`
}

type statusDetails struct {
	Allowed         *bool           `json:"allowed"`
	LimitReached    *bool           `json:"limit_reached"`
	PrimaryWindow   *windowSnapshot `json:"primary_window"`
	SecondaryWindow *windowSnapshot `json:"secondary_window"`
}

type windowSnapshot struct {
	UsedPercent        float64 `json:"used_percent"`
	LimitWindowSeconds int64   `json:"limit_window_seconds"`
	ResetAfterSeconds  int64   `json:"reset_after_seconds"`
	ResetAt            int64   `json:"reset_at"`
}

type creditDetails struct {
	HasCredits bool    `json:"has_credits"`
	Unlimited  bool    `json:"unlimited"`
	Balance    *string `json:"balance"`
}

type additionalLimit struct {
	LimitName      string         `json:"limit_name"`
	MeteredFeature string         `json:"metered_feature"`
	RateLimit      *statusDetails `json:"rate_limit"`
}

// Parse decodes a /wham/usage body into a snapshot captured at now.
func Parse(body []byte, now time.Time) (Snapshot, error) {
	var p payload
	if err := json.Unmarshal(body, &p); err != nil {
		return Snapshot{}, fmt.Errorf("failed to decode the usage response: %w", err)
	}
	if p.PlanType == "" && p.RateLimit == nil && len(p.AdditionalRateLimits) == 0 {
		return Snapshot{}, errors.New("the usage response has no plan and no limits")
	}
	s := Snapshot{Plan: p.PlanType, CapturedAt: now, Limits: []Limit{limit(CodexLimitID, "", p.RateLimit)}}
	for _, a := range p.AdditionalRateLimits {
		s.Limits = append(s.Limits, limit(normalizeID(a.MeteredFeature), a.LimitName, a.RateLimit))
	}
	if c := p.Credits; c != nil {
		s.Credits = &Credits{HasCredits: c.HasCredits, Unlimited: c.Unlimited}
		if c.Balance != nil {
			s.Credits.Balance = *c.Balance
		}
	}
	if p.RateLimitReachedType != nil {
		s.ReachedType = p.RateLimitReachedType.Type
	}

	return s, nil
}

func limit(id, name string, d *statusDetails) Limit {
	l := Limit{ID: id, Name: name}
	if d == nil {
		return l
	}
	l.Allowed, l.Reached = d.Allowed, d.LimitReached
	l.Primary, l.Secondary = window(d.PrimaryWindow), window(d.SecondaryWindow)

	return l
}

// window converts seconds to minutes rounding up, as Codex does
// (backend-client/src/client.rs:719-732 and 789-796).
func window(w *windowSnapshot) *Window {
	if w == nil {
		return nil
	}
	out := &Window{UsedPercent: w.UsedPercent}
	if w.LimitWindowSeconds > 0 {
		out.Minutes = (w.LimitWindowSeconds + 59) / 60
	}
	if w.ResetAt > 0 {
		out.ResetsAt = time.Unix(w.ResetAt, 0)
	}

	return out
}
