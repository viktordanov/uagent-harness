// Package ini parses INI files; see README.md for the rules.
package ini

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// File is a parsed INI file: section name to key to value.
type File map[string]map[string]string

// Get returns the value of key in section.
func (f File) Get(section, key string) (string, bool) {
	v, ok := f[section][strings.ToLower(key)]

	return v, ok
}

// ParseError is a syntax error at a line.
type ParseError struct {
	Line int
	Msg  string
}

func (e *ParseError) Error() string { return fmt.Sprintf("ini: line %d: %s", e.Line, e.Msg) }

// Parse reads an INI file.
func Parse(r io.Reader) (File, error) {
	f := File{}
	section := ""
	sc := bufio.NewScanner(r)
	n := 0
	for sc.Scan() {
		n++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			section = strings.TrimSpace(strings.Trim(line, "[]"))

			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return nil, &ParseError{Line: n, Msg: "expected key = value"}
		}
		if f[section] == nil {
			f[section] = map[string]string{}
		}
		f[section][strings.TrimSpace(key)] = strings.TrimSpace(value)
	}

	return f, sc.Err()
}
