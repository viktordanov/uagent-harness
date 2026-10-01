// Package roster reads team rosters: one member per line, tab-separated
// name and team.
package roster

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// Member is one roster row.
type Member struct {
	Name string `json:"name"`
	Team string `json:"team"`
}

// Load reads a roster. Blank lines are skipped.
func Load(r io.Reader) ([]Member, error) {
	var out []Member
	sc := bufio.NewScanner(r)
	n := 0
	for sc.Scan() {
		n++
		line := strings.TrimRight(sc.Text(), "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		cols := strings.Split(line, "\t")
		if len(cols) != 2 {
			return nil, fmt.Errorf("line %d: want 2 columns, got %d", n, len(cols))
		}
		out = append(out, Member{Name: cols[0], Team: cols[1]})
	}

	return out, sc.Err()
}
