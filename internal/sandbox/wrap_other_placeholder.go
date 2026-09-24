//go:build !darwin && !linux

package sandbox

// wrap is a placeholder until wrap_other.go lands from the macOS branch.
func wrap(Policy, []string) ([]string, error) { return nil, ErrUnavailable }
