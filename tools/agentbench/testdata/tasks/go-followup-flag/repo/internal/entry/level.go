package entry

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Level is a log level; a higher one is more severe.
type Level int

// The levels.
const (
	Debug Level = iota
	Info
	Warn
	Error
)

var names = []string{"debug", "info", "warn", "error"}

// ParseLevel reads a level's name, in any case; "warning" is warn.
func ParseLevel(s string) (Level, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "warning" {
		s = "warn"
	}
	for i, n := range names {
		if n == s {
			return Level(i), nil
		}
	}

	return 0, fmt.Errorf("unknown level %q", s)
}

func (l Level) String() string {
	if l < 0 || int(l) >= len(names) {
		return fmt.Sprintf("level(%d)", int(l))
	}

	return names[l]
}

// UnmarshalJSON reads a level's name.
func (l *Level) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	v, err := ParseLevel(s)
	*l = v

	return err
}

// MarshalJSON writes a level's name.
func (l Level) MarshalJSON() ([]byte, error) { return json.Marshal(l.String()) }
