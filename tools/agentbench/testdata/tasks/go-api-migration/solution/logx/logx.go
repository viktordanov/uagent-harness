// Package logx is the shop's logger.
package logx

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
)

var (
	mu  sync.Mutex
	out io.Writer = os.Stderr
)

// SetOutput sends log lines to w.
func SetOutput(w io.Writer) {
	mu.Lock()
	defer mu.Unlock()
	out = w
}

// Info writes a structured line: level=info msg="..." key=value ...
// msg must be a constant; put variable data in kv (pairs of a string key
// and any value).
func Info(msg string, kv ...any) { write("info", msg, kv) }

// Error is Info at level error.
func Error(msg string, kv ...any) { write("error", msg, kv) }

func write(level, msg string, kv []any) {
	var b strings.Builder
	fmt.Fprintf(&b, "level=%s msg=%q", level, msg)
	for i := 0; i+1 < len(kv); i += 2 {
		fmt.Fprintf(&b, " %v=%v", kv[i], kv[i+1])
	}
	if len(kv)%2 == 1 {
		fmt.Fprintf(&b, " !BADKEY=%v", kv[len(kv)-1])
	}
	mu.Lock()
	defer mu.Unlock()
	fmt.Fprintln(out, b.String())
}
