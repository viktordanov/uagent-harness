//go:build !darwin && !linux

package clipboard

import "runtime"

// New is a reader that says pasting an image is not supported here.
func New(Exec, func(string) (string, error), func(string) string) Reader {
	return unsupported{os: runtime.GOOS}
}
