package sandbox

// The functions below are implemented per platform and concern:
//
//	seatbelt.go     SeatbeltProfile(p Policy) (profile string, params []string)  — pure, any platform
//	bwrap.go        BwrapArgs(p Policy) []string                                 — pure, any platform
//	wrap_darwin.go  wrap(p, argv): sandbox-exec -p <profile> <params> -- argv
//	wrap_linux.go   wrap(p, argv): bwrap <args> -- argv, or ErrUnavailable without bwrap
//	wrap_other.go   wrap(p, argv): ErrUnavailable
//	denied.go       Denied(exitCode int, output string) bool                     — Codex's heuristic
//	env.go          EnvPolicy and (EnvPolicy).Apply(environ []string) []string   — Codex's shell_environment_policy
