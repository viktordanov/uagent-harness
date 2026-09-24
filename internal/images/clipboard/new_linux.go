package clipboard

// New is the Linux reader: wl-paste or xclip.
func New(run Exec, lookPath func(string) (string, error), getenv func(string) string) Reader {
	return Linux{Exec: run, LookPath: lookPath, Getenv: getenv}
}
