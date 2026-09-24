//go:build linux

package sandbox

// wrap is a placeholder until the bubblewrap wrapper (wrap_linux.go) lands;
// delete this file when merging it.
func wrap(Policy, []string) ([]string, error) { return nil, ErrUnavailable }
