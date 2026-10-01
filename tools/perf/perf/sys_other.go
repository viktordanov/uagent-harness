//go:build !darwin && !linux

package perf

import "syscall"

// diskAndWakeups are unknown here.
func diskAndWakeups(syscall.Rusage) (disk, wakeups uint64) { return 0, 0 }
