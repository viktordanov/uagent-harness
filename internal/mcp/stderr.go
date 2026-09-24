package mcp

import (
	"bytes"
	"context"
	"log/slog"
	"sync"
)

// maxStderrLine bounds a line kept while waiting for its end, so a server
// that never ends a line cannot grow it without limit.
const maxStderrLine = 64 << 10

// stderrLog logs a stdio server's standard error one line at a time, as
// Codex does ("MCP server stderr"), instead of writing it to the terminal.
type stderrLog struct {
	logger *slog.Logger
	server string

	mu  sync.Mutex
	buf []byte
}

func (l *stderrLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.buf = append(l.buf, p...)
	for {
		i := bytes.IndexByte(l.buf, '\n')
		if i < 0 {
			break
		}
		l.log(l.buf[:i])
		l.buf = l.buf[i+1:]
	}
	if len(l.buf) > maxStderrLine {
		l.log(l.buf)
		l.buf = nil
	}

	return len(p), nil
}

func (l *stderrLog) log(line []byte) {
	if line = bytes.TrimRight(line, "\r"); len(line) > 0 {
		l.logger.LogAttrs(context.Background(), slog.LevelInfo, "MCP server stderr",
			slog.String("server", l.server),
			slog.String("line", string(line)))
	}
}
