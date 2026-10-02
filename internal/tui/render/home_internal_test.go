package render

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestHome: a path is shown as ~ only at or below the home directory, and
// without one it is shown whole.
func TestHome(t *testing.T) {
	for _, c := range []struct{ home, path, want string }{
		{"/Users/vik", "/Users/vik", "~"},
		{"/Users/vik", "/Users/vik/proj", "~/proj"},
		{"/Users/vik/", "/Users/vik/proj", "~/proj"},
		{"/Users/vik", "/Users/vik-other/proj", "/Users/vik-other/proj"},
		{"/Users/vik", "/Users/vikx", "/Users/vikx"},
		{"/Users/vik", "/srv/proj", "/srv/proj"},
		{"", "/Users/vik/proj", "/Users/vik/proj"},
	} {
		assert.Equal(t, c.want, home(c.home, c.path), "%q under %q", c.path, c.home)
	}
}
