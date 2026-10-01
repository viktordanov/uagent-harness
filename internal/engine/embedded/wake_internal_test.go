package embedded

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWakePolicyHoldsByDefault(t *testing.T) {
	p := readExperiments(func(string) string { return "" }).wakePolicy()
	assert.Equal(t, wakeHold, p.Hold)
	assert.Zero(t, p.ReleaseQuick)
	assert.NotNil(t, p.Progress)

	p = readExperiments(func(string) string { return "other, wake-hold-60s" }).wakePolicy()
	assert.Equal(t, shortWakeHold, p.Hold)
	assert.Zero(t, p.ReleaseQuick)

	p = readExperiments(func(string) string { return "wake-release-quick" }).wakePolicy()
	assert.Equal(t, wakeHold, p.Hold)
	assert.Equal(t, releaseQuick, p.ReleaseQuick)
}
