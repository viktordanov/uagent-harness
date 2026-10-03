package contextprep

import (
	"context"
	"fmt"

	"github.com/viktordanov/uah-core/harness/operation"
)

// Harness is the "harness" block: how to use uah's tools well, starting
// with the Bash tool's max_output_length, whose default stays as it is.
type Harness struct{}

// Name is the block's name.
func (Harness) Name() string { return "harness" }

// Prepare returns the guidance; it is the same for every session.
func (Harness) Prepare(context.Context, Facts) string {
	return fmt.Sprintf("Bash output: max_output_length caps each output field at that many characters (default %d); "+
		"cut output keeps its head and tail around a marker, with the path of the file that holds all of it.\n"+
		"- Leave it unset when you need the whole output: a file you will edit, a failing test's full report.\n"+
		"- Set it only for noisy commands (builds, logs, broad searches), sized to what you will actually read.\n"+
		"- When output was cut, narrow the command (a pattern, a line range, one test) or read the saved file in parts; "+
		"do not run it again with a bigger limit.", operation.DefaultMaxOutputLength)
}
