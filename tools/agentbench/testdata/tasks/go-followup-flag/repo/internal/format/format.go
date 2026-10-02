// Package format prints entries.
package format

import (
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
	"time"

	"example.com/logq/internal/entry"
)

// A Printer writes one entry.
type Printer interface {
	Print(w io.Writer, e entry.Entry) error
}

// New returns the printer of a format: text or json.
func New(name string) (Printer, error) {
	switch name {
	case "text":
		return text{}, nil
	case "json":
		return jsonLines{}, nil
	}

	return nil, fmt.Errorf("unknown format %q (want text or json)", name)
}

type text struct{}

// Print writes "time LEVEL msg k=v ...", the fields sorted by key.
func (text) Print(w io.Writer, e entry.Entry) error {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %-5s %s", e.Time.UTC().Format(time.RFC3339), strings.ToUpper(e.Level.String()), e.Msg)
	for _, k := range slices.Sorted(maps.Keys(e.Fields)) {
		fmt.Fprintf(&b, " %s=%v", k, e.Fields[k])
	}
	b.WriteByte('\n')
	_, err := io.WriteString(w, b.String())

	return err
}

type jsonLines struct{}

func (jsonLines) Print(w io.Writer, e entry.Entry) error {
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	_, err = w.Write(append(b, '\n'))

	return err
}
