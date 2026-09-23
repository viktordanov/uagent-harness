package main

// Framework-agnostic end-to-end snapshot test: run each spike binary in a real
// pty (via runOnce), replay the captured byte stream through a VT emulator
// (charmbracelet/x/vt) and assert on the final screen. Works identically for
// every framework, including tcell v3 / vaxis / ultraviolet which have no
// public headless screen. Requires ./bench.sh build first.

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/vt"
)

func finalScreen(t *testing.T, dump []byte) string {
	t.Helper()
	if d := bytes.LastIndex(dump, []byte("DONE")); d >= 0 {
		cut := len(dump)
		for _, seq := range []string{"\x1b[2J", "\x1b[?1049l", "\x1b[J"} {
			if i := bytes.Index(dump[d:], []byte(seq)); i >= 0 && d+i < cut {
				cut = d + i
			}
		}
		dump = dump[:cut]
	}
	e := vt.NewEmulator(120, 40)
	go io.Copy(io.Discard, e)
	_, _ = e.Write(dump)
	return e.String()
}

func TestSpikesRenderSameFinalFrame(t *testing.T) {
	for _, b := range []string{"bt", "btfast", "tview", "tcell", "vaxis", "uv", "gocui"} {
		t.Run(b, func(t *testing.T) {
			bin := filepath.Join("..", "bin", b)
			if _, err := os.Stat(bin); err != nil {
				t.Skip("build first: ./bench.sh build")
			}
			dump := filepath.Join(t.TempDir(), "out")
			r := runOnce(bin, "-n 200 -exit -delay 50ms", 120, 40, true, false, 0, 20*time.Second, "", dump)
			if r.TimedOut {
				t.Fatal("timed out")
			}
			raw, _ := os.ReadFile(dump)
			scr := finalScreen(t, raw)
			lines := strings.Split(scr, "\n")
			last := lines[len(lines)-1]
			if !strings.Contains(last, "events 200/200") || !strings.Contains(last, "DONE @@") {
				t.Fatalf("status line = %q", last)
			}
			if !strings.Contains(scr, "▶ grep path=internal/pkg49/file_00199.go") {
				t.Fatalf("last tool line missing:\n%s", scr)
			}
		})
	}
}
