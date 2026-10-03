package contextprep

import (
	"context"
	"fmt"
)

// Harness is the "harness" block: how to use uah's tools well, starting
// with the Bash tool's max_output_length, whose default stays as it is.
type Harness struct {
	// MaxOutputLength is max_output_length's default, or 0 when it is not
	// known.
	MaxOutputLength int
}

// Name is the block's name.
func (Harness) Name() string { return "harness" }

// Prepare returns the guidance; it is the same for every session.
func (h Harness) Prepare(context.Context, Facts) string {
	limit := ""
	if h.MaxOutputLength > 0 {
		limit = fmt.Sprintf(" (default %d)", h.MaxOutputLength)
	}

	return "Bash output: max_output_length caps each output field at that many characters" + limit + "; " +
		"cut output keeps its head and tail around a marker, with the path of the file that holds all of it.\n" +
		"- Leave it unset when you need the whole output: a file you will edit, a failing test's full report.\n" +
		"- Set it only for noisy commands (builds, logs, broad searches), sized to what you will actually read.\n" +
		"- When output was cut, narrow the command (a pattern, a line range, one test) or read the saved file in parts; " +
		"do not run it again with a bigger limit."
}
