// Adapted from openai/codex rust-v0.156.1 (Apache-2.0):
// codex-rs/sandboxing/src/denial.rs.

package sandbox

import "strings"

// deniedKeywords are Codex's SANDBOX_DENIED_KEYWORDS, matched in lower case.
var deniedKeywords = []string{
	"operation not permitted",
	"permission denied",
	"read-only file system",
	"seccomp",
	"sandbox",
	"landlock",
	"failed to write file",
}

// networkKeywords are uah's addition. Codex's seccomp filter makes connect
// fail with "operation not permitted", which the keywords above catch; bwrap's
// --unshare-net and Seatbelt's network deny instead leave no DNS and no route,
// so resolvers and clients print these.
var networkKeywords = []string{
	"could not resolve host",
	"temporary failure in name resolution",
	"name or service not known",
	"network is unreachable",
}

// Denied reports whether a failed sandboxed command was probably stopped by
// the sandbox, as Codex guesses: a non-zero exit code and output that
// mentions a denial. There is no certain way to tell; a command can print
// "permission denied" for its own reasons. Codex checks the keywords before
// its quick-reject exit codes (2, 126, 127), so those codes never override a
// keyword; its 128+SIGSYS rule is for its seccomp filter, which uah has not.
func Denied(exitCode int, output string) bool {
	if exitCode == 0 {
		return false
	}
	lower := strings.ToLower(output)
	for _, k := range deniedKeywords {
		if strings.Contains(lower, k) {
			return true
		}
	}
	for _, k := range networkKeywords {
		if strings.Contains(lower, k) {
			return true
		}
	}

	return false
}
