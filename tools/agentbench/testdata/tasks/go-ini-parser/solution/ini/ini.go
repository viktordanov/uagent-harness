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
	next := func() (string, bool) {
		if !sc.Scan() {
			return "", false
		}
		n++

		return strings.TrimSpace(strings.TrimSuffix(sc.Text(), "\r")), true
	}
	for {
		line, ok := next()
		if !ok {
			break
		}
		start := n
		if line == "" || line[0] == '#' || line[0] == ';' {
			continue
		}
		if line[0] == '[' {
			if !strings.HasSuffix(line, "]") {
				return nil, &ParseError{Line: start, Msg: "section header without ]"}
			}
			section = strings.TrimSpace(line[1 : len(line)-1])
			if section == "" {
				return nil, &ParseError{Line: start, Msg: "empty section name"}
			}

			continue
		}
		key, value, ok := strings.Cut(line, "=")
		key, value = strings.ToLower(strings.TrimSpace(key)), strings.TrimSpace(value)
		if !ok || key == "" {
			return nil, &ParseError{Line: start, Msg: "expected key = value"}
		}
		if strings.HasPrefix(value, `"`) {
			v, err := unquote(value)
			if err != nil {
				return nil, &ParseError{Line: start, Msg: err.Error()}
			}
			value = v
		} else {
			for strings.HasSuffix(value, `\`) {
				value = strings.TrimSuffix(value, `\`)
				more, ok := next()
				if !ok {
					break
				}
				value += " " + more
			}
		}
		if f[section] == nil {
			f[section] = map[string]string{}
		}
		f[section][key] = value
	}

	return f, sc.Err()
}

func unquote(s string) (string, error) {
	var b strings.Builder
	for i := 1; i < len(s); i++ {
		switch c := s[i]; {
		case c == '"':
			if i != len(s)-1 {
				return "", fmt.Errorf("text after the closing quote")
			}

			return b.String(), nil
		case c == '\\' && i+1 < len(s) && (s[i+1] == '"' || s[i+1] == '\\'):
			b.WriteByte(s[i+1])
			i++
		default:
			b.WriteByte(c)
		}
	}

	return "", fmt.Errorf("missing closing quote")
}
