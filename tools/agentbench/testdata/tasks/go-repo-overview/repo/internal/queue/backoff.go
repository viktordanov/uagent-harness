package queue

import "time"

// maxDelay caps the backoff.
const maxDelay = 30 * time.Second

// backoff is base·2^(attempt-1), capped at maxDelay.
func backoff(base time.Duration, attempt int) time.Duration {
	d := base << (attempt - 1)
	if d <= 0 || d > maxDelay {
		return maxDelay
	}

	return d
}
