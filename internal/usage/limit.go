package usage

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// resetsAtPattern finds "resets_at": 1790426679 in an error text that kept
// the 429's body.
var resetsAtPattern = regexp.MustCompile(`"resets_at"\s*:\s*(\d+)`)

// LimitReachedIn reads a run's failure text, which the runner reports as
// text only ("responses API error usage_limit_reached: …", or "… failed with
// status 429: …" and the body when it was not JSON). It reports whether the
// text is the usage limit, and the reset time and plan when the text kept
// them.
func LimitReachedIn(text string) (LimitReached, bool) {
	lower := strings.ToLower(text)
	switch {
	case strings.Contains(lower, "usage_limit_reached"):
	case strings.Contains(lower, "429") && strings.Contains(lower, "usage limit"):
	default:
		return LimitReached{}, false
	}
	if i, j := strings.Index(text, "{"), strings.LastIndex(text, "}"); i >= 0 && j > i {
		if r, ok := ParseLimitReached([]byte(text[i : j+1])); ok {
			return r, true
		}
	}
	var r LimitReached
	if m := resetsAtPattern.FindStringSubmatch(text); m != nil {
		if at, err := strconv.ParseInt(m[1], 10, 64); err == nil && at > 0 {
			r.ResetsAt = time.Unix(at, 0)
		}
	}

	return r, true
}
