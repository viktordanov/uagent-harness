package embedded

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWakePolicyHolds(t *testing.T) {
	p := wakePolicy()
	assert.Equal(t, wakeHold, p.Hold)
	assert.NotNil(t, p.Progress)
}

func TestPreambleWakeSetsTheHold(t *testing.T) {
	assert.Zero(t, readExperiments(func(string) string { return "" }).builderOptions().Hold)
	assert.Equal(t, wakeHold, readExperiments(func(string) string { return "other, preamble-wake" }).builderOptions().Hold)
}
