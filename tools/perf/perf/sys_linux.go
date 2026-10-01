package perf

import (
	"bytes"
	"os"
	"strconv"
	"syscall"
)

// diskAndWakeups are write_bytes from /proc/self/io and the voluntary
// context switches.
func diskAndWakeups(self syscall.Rusage) (disk, wakeups uint64) {
	if data, err := os.ReadFile("/proc/self/io"); err == nil {
		for line := range bytes.SplitSeq(data, []byte("\n")) {
			if v, ok := bytes.CutPrefix(line, []byte("write_bytes: ")); ok {
				disk, _ = strconv.ParseUint(string(v), 10, 64)
			}
		}
	}

	return disk, uint64(self.Nvcsw) //nolint:gosec // a count, never negative
}
