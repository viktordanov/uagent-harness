package clipboard

// New is the macOS reader: osascript.
func New(run Exec, _ func(string) (string, error), _ func(string) string) Reader {
	return MacOS{Exec: run}
}
