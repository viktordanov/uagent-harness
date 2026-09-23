//go:build darwin

package main

/*
#include <libproc.h>
#include <mach/mach_time.h>
#include <sys/resource.h>

static int pid_usage(int pid, unsigned long long *user_ns, unsigned long long *sys_ns, unsigned long long *resident, unsigned long long *footprint) {
	struct rusage_info_v2 ri;
	if (proc_pid_rusage(pid, RUSAGE_INFO_V2, (rusage_info_t *)&ri) != 0) return -1;
	mach_timebase_info_data_t tb;
	mach_timebase_info(&tb);
	*user_ns = ri.ri_user_time * tb.numer / tb.denom;
	*sys_ns = ri.ri_system_time * tb.numer / tb.denom;
	*resident = ri.ri_resident_size;
	*footprint = ri.ri_phys_footprint;
	return 0;
}
*/
import "C"

import "time"

// pidUsage returns precise CPU time (user+sys), resident size and phys
// footprint (bytes) of a live process via proc_pid_rusage.
func pidUsage(pid int) (cpu time.Duration, rssKB, footprintKB int64, ok bool) {
	var u, s, r, f C.ulonglong
	if C.pid_usage(C.int(pid), &u, &s, &r, &f) != 0 {
		return 0, 0, 0, false
	}
	return time.Duration(u + s), int64(r) / 1024, int64(f) / 1024, true
}
