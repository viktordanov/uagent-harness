// Command replay feeds a raw pty dump into a VT emulator (charmbracelet/x/vt)
// and prints the screen as it was just before the app left the alternate
// screen. Used to check that all spikes render the same final frame.
package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/charmbracelet/x/vt"
)

func main() {
	b, err := os.ReadFile(os.Args[1])
	if err != nil {
		panic(err)
	}
	cols, rows := 120, 40
	if len(os.Args) > 3 {
		cols, _ = strconv.Atoi(os.Args[2])
		rows, _ = strconv.Atoi(os.Args[3])
	}
	// Cut right before the teardown (screen clear or alt-screen exit) that
	// follows the last frame containing DONE.
	if d := bytes.LastIndex(b, []byte("DONE")); d >= 0 {
		cut := len(b)
		for _, seq := range []string{"\x1b[2J", "\x1b[?1049l", "\x1b[J"} {
			if i := bytes.Index(b[d:], []byte(seq)); i >= 0 && d+i < cut {
				cut = d + i
			}
		}
		b = b[:cut]
	}
	e := vt.NewEmulator(cols, rows)
	go io.Copy(io.Discard, e)
	_, _ = e.Write(b)
	fmt.Println(e.String())
}
