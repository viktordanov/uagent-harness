package approval

import (
	"slices"
	"strings"
)

// bannedPrefixes are the prefixes, words joined by spaces, Codex never proposes for "don't ask again",
// because they would allow arbitrary code: shells and interpreters with
// their inline-code flags, bare git, rm, sudo, env, and script runners
// (codex-rs/core/src/exec_policy.rs, BANNED_PREFIX_SUGGESTIONS).
var bannedPrefixes = []string{
	"/bin/bash", "/bin/bash -c", "/bin/bash -lc", "/bin/sh", "/bin/sh -c", "/bin/sh -lc", "/bin/zsh",
	"/bin/zsh -c", "/bin/zsh -lc", "Rscript", "bash", "bash -c", "bash -lc", "bun", "bun -e",
	"bun run", "dash", "dash -c", "deno", "deno eval", "env", "fish", "fish -c", "git", "julia",
	"julia -e", "ksh", "ksh -c", "lua", "lua -e", "node", "node -e", "nodejs", "nodejs -e", "npm run",
	"osascript", "perl", "perl -e", "php", "php -r", "pnpm run", "pwsh", "pwsh -c", "pwsh -Command",
	"py", "py -3", "pypy", "pypy3", "python", "python -", "python -c", "python3", "python3 -",
	"python3 -c", "rm", "ruby", "ruby -e", "sh", "sh -c", "sh -lc", "sudo", "yarn run", "zsh",
	"zsh -c", "zsh -lc",
}

// proposePrefix is the prefix "don't ask again" offers, as Codex derives
// it: the model's suggested prefix when it covers every command and is not
// banned, else the whole command when it is one simple command. Commands
// Split could not parse get no proposal.
func proposePrefix(suggested []string, commands [][]string) []string {
	if len(suggested) > 0 && !banned(suggested) && coversAll(suggested, commands) {
		return slices.Clone(suggested)
	}
	if len(commands) == 1 && !banned(commands[0]) {
		return slices.Clone(commands[0])
	}

	return nil
}

func banned(prefix []string) bool {
	return slices.Contains(bannedPrefixes, strings.Join(prefix, " "))
}

func coversAll(prefix []string, commands [][]string) bool {
	if len(commands) == 0 {
		return false
	}
	for _, words := range commands {
		if len(words) < len(prefix) || !slices.Equal(words[:len(prefix)], prefix) {
			return false
		}
	}

	return true
}
