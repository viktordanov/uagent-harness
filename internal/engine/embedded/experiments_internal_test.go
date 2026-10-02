package embedded

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestReadExperiments(t *testing.T) {
	assert.Equal(t, experiments{leanRule: leanR3}, readExperiments(func(string) string { return " unknown, lean-rule=r3" }))
	assert.Equal(t, experiments{leanRule: defaultLeanRule}, readExperiments(func(string) string { return "lean-rule=r9" }), "an unknown rule keeps the default")
	assert.Equal(t, experiments{leanRule: leanR1}, readExperiments(func(string) string { return "" }))
}
