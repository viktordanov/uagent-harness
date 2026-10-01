// Package roster reads team rosters: one member per line, tab-separated
// name, team, and an optional role.
package roster

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// DefaultRole is the role of a row without one.
const DefaultRole = "member"

// Member is one roster row.
type Member struct {
	Name string `json:"name"`
	Team string `json:"team"`
	Role string `json:"role"`
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
		if len(cols) != 2 && len(cols) != 3 {
			return nil, fmt.Errorf("line %d: want 2 or 3 columns, got %d", n, len(cols))
		}
		m := Member{Name: cols[0], Team: cols[1], Role: DefaultRole}
		if len(cols) == 3 && cols[2] != "" {
			m.Role = cols[2]
		}
		out = append(out, m)
	}

	return out, sc.Err()
}
