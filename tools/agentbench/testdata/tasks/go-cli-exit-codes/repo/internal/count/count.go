// Package count counts lines in files.
package count

import (
	"bufio"
	"os"
)

// Result is one file's count, or why it could not be counted.
type Result struct {
	Name  string
	Lines int
	Err   error
}

// Files counts the lines of each file, in order.
func Files(names []string) []Result {
	out := make([]Result, 0, len(names))
	for _, n := range names {
		lines, err := lines(n)
		out = append(out, Result{Name: n, Lines: lines, Err: err})
	}

	return out
}

func lines(name string) (int, error) {
	f, err := os.Open(name)
	if err != nil {
		return 0, nil
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	n := 0
	for sc.Scan() {
		n++
	}

	return n, sc.Err()
}
