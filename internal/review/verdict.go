package review

import (
	"encoding/json/v2"
	"fmt"
	"strings"
)

// answer is the model's JSON, Codex's GuardianAssessment. Only outcome is
// required.
type answer struct {
	RiskLevel         string `json:"risk_level"`
	UserAuthorization string `json:"user_authorization"`
	Outcome           string `json:"outcome"`
	Rationale         string `json:"rationale"`
}

// Parse reads the model's answer: one JSON object with Codex's fields and
// nothing else, optionally in a Markdown code fence. Unknown fields and
// values are errors.
func Parse(text string) (Verdict, error) {
	body := unfence(strings.TrimSpace(text))
	var a answer
	if err := json.Unmarshal([]byte(body), &a, json.RejectUnknownMembers(true)); err != nil {
		return Verdict{}, fmt.Errorf("failed to parse the review answer as JSON: %w", err)
	}
	v := Verdict{Outcome: Outcome(a.Outcome), Risk: Risk(a.RiskLevel), Authorization: a.UserAuthorization, Reason: strings.TrimSpace(a.Rationale)}
	if v.Outcome != Allow && v.Outcome != Deny {
		return Verdict{}, fmt.Errorf("invalid review outcome %q", a.Outcome)
	}
	switch v.Risk {
	case RiskLow, RiskMedium, RiskHigh, RiskCritical:
	case "":
		// Codex's defaults: an allow without a level is low, a deny high.
		v.Risk = RiskHigh
		if v.Outcome == Allow {
			v.Risk = RiskLow
		}
	default:
		return Verdict{}, fmt.Errorf("invalid review risk_level %q", a.RiskLevel)
	}
	switch v.Authorization {
	case "unknown", "low", "medium", "high":
	case "":
		v.Authorization = "unknown"
	default:
		return Verdict{}, fmt.Errorf("invalid review user_authorization %q", a.UserAuthorization)
	}
	if v.Reason == "" {
		v.Reason = defaultReason(v.Outcome)
	}

	return v, nil
}

func defaultReason(o Outcome) string {
	if o == Allow {
		return "Auto-review returned a low-risk allow decision."
	}

	return "Auto-review returned a deny decision without a rationale."
}

// unfence strips one surrounding Markdown code fence.
func unfence(s string) string {
	if !strings.HasPrefix(s, "```") || !strings.HasSuffix(s, "```") || len(s) < 6 {
		return s
	}
	s = strings.TrimSuffix(s[3:], "```")
	if i := strings.IndexByte(s, '\n'); i >= 0 && !strings.Contains(s[:i], "{") {
		s = s[i+1:]
	}

	return strings.TrimSpace(s)
}
