package embedded

import (
	"testing"
	"time"
)

// SlowCanceledCalls delays the end of each canceled model request by d, as a
// loaded machine may, until the test ends. Tests that run
// alongside it may slow down too.
func SlowCanceledCalls(t *testing.T, d time.Duration) {
	t.Helper()
	canceledEndDelay.Store(int64(d))
	t.Cleanup(func() { canceledEndDelay.Store(0) })
}
