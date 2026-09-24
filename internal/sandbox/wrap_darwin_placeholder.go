//go:build darwin

package sandbox

// wrap is a placeholder until wrap_darwin.go lands from the macOS branch.
func wrap(Policy, []string) ([]string, error) { return nil, ErrUnavailable }
