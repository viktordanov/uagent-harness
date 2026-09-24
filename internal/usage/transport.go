package usage

import (
	"net/http"
	"time"
)

// Transport wraps a round tripper and reports the rate-limit headers of
// every response that has them, so usage comes for free with each model
// call. It never reads or changes the body. It fits only a client whose
// *http.Client uah builds itself (docs/design/usage.md, option B).
type Transport struct {
	// Base defaults to http.DefaultTransport.
	Base http.RoundTripper
	// Observe receives each snapshot; it must not block.
	Observe func(Snapshot)
	// Now defaults to time.Now.
	Now func() time.Time
}

// RoundTrip implements http.RoundTripper.
func (t Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.Base
	if base == nil {
		base = http.DefaultTransport
	}
	resp, err := base.RoundTrip(req)
	if err != nil || t.Observe == nil {
		return resp, err //nolint:wrapcheck // a transport returns its base's errors unchanged
	}
	now := time.Now
	if t.Now != nil {
		now = t.Now
	}
	if s, ok := ParseHeaders(resp.Header, now()); ok {
		t.Observe(s)
	}

	return resp, nil
}
