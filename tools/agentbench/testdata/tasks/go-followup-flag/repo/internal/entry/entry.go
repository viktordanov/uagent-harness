// Package entry reads log entries from JSON lines.
package entry

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"time"
)

// Entry is one log line.
type Entry struct {
	Time   time.Time      `json:"time"`
	Level  Level          `json:"level"`
	Msg    string         `json:"msg"`
	Fields map[string]any `json:"fields,omitempty"`
}

// ReadAll reads entries until EOF. A line that is not a JSON object is
// skipped with a warning on warn.
func ReadAll(r io.Reader, warn io.Writer) ([]Entry, error) {
	var out []Entry
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	n := 0
	for sc.Scan() {
		n++
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var e Entry
		if err := json.Unmarshal(line, &e); err != nil {
			fmt.Fprintf(warn, "logq: line %d: skipped: %v\n", n, err)

			continue
		}
		out = append(out, e)
	}

	return out, sc.Err()
}
