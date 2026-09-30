// Adapted from openai/codex rust-v0.159.1 (Apache License 2.0, Copyright
// 2025 OpenAI): codex-rs/shell-command/src/parse_command.rs
// (summarize_main_tokens and the helpers it calls).

package cmdparse

import (
	"slices"
	"strings"
)

// summarizer parses one command whose first word it is registered for;
// main is the whole command and tail its arguments.
type summarizer func(main, tail []string) []Parsed

// summarizers are the commands Codex recognizes, by first word.
var summarizers map[string]summarizer

func init() {
	summarizers = map[string]summarizer{
		"ls": listWith(lsFlags), "eza": listWith(ezaFlags), "exa": listWith(ezaFlags),
		"tree": listWith([]string{"-L", "-P", "-I", "--charset", "--filelimit", "--sort"}),
		"du":   listWith([]string{"-d", "--max-depth", "-B", "--block-size", flagExclude, flagTimeStyle}),
		"rg":   summarizeRg, "rga": summarizeRg, "ripgrep-all": summarizeRg,
		"git":  summarizeGit,
		"fd":   summarizeFd,
		"find": summarizeFind,
		"grep": parseGrepLike, "egrep": parseGrepLike, "fgrep": parseGrepLike,
		"ag": summarizeAg, "ack": summarizeAg, "pt": summarizeAg,
		"cat":   summarizeCat,
		"bat":   readWith([]string{"--theme", "--language", "--style", "--terminal-width", "--tabs", "--line-range", "--map-syntax"}),
		"less":  readWith([]string{"-p", "-P", "-x", "-y", "-z", "-j", "--pattern", "--prompt", "--tabs", "--shift", "--jump-target"}),
		"more":  readWith(nil),
		cmdHead: summarizeHead, "tail": summarizeTail,
		"awk":  summarizeAwk,
		"nl":   summarizeNl,
		cmdSed: summarizeSed,
	}
	summarizers["batcat"] = summarizers["bat"]
}

var (
	lsFlags  = []string{"-I", "-w", "--block-size", "--format", flagTimeStyle, "--color", "--quoting-style"}
	ezaFlags = []string{"-I", "--ignore-glob", "--color", "--sort", flagTimeStyle, "--time"}
	rgFlags  = []string{"-g", "--glob", "--iglob", "-t", "--type", "--type-add", "--type-not", "-m", "--max-count", "-A", "-B", "-C", "--context", "--max-depth"}
)

// summarizeMainTokens parses one command of a sequence.
func summarizeMainTokens(main []string) []Parsed {
	if len(main) == 0 {
		return []Parsed{unknown(main)}
	}
	head, tail := main[0], main[1:]
	if s, ok := summarizers[head]; ok {
		return s(main, tail)
	}
	if isPythonCommand(head) && pythonWalksFiles(tail) {
		return []Parsed{{Kind: ListFiles, Cmd: join(main)}}
	}

	return []Parsed{unknown(main)}
}

func unknown(main []string) Parsed { return Parsed{Kind: Unknown, Cmd: join(main)} }

func read(main []string, p string) Parsed {
	return Parsed{Kind: Read, Cmd: join(main), Name: shortDisplayPath(p), Path: p}
}

// listWith lists the first operand, skipping the values of flags.
func listWith(flags []string) summarizer {
	return func(main, tail []string) []Parsed {
		p := Parsed{Kind: ListFiles, Cmd: join(main)}
		if op, ok := firstNonFlagOperand(tail, flags); ok {
			p.Path, p.Paths = shortDisplayPath(op), []string{op}
		}

		return []Parsed{p}
	}
}

// readWith reads the one operand, skipping the values of flags.
func readWith(flags []string) summarizer {
	return func(main, tail []string) []Parsed {
		if op, ok := singleNonFlagOperand(tail, flags); ok {
			return []Parsed{read(main, op)}
		}

		return []Parsed{unknown(main)}
	}
}

func summarizeRg(main, tail []string) []Parsed {
	args := trimAtConnector(tail)
	var nonFlags []string
	for _, a := range skipFlagValues(args, rgFlags) {
		if !strings.HasPrefix(a, "-") {
			nonFlags = append(nonFlags, a)
		}
	}
	if contains(args, "--files") {
		p := Parsed{Kind: ListFiles, Cmd: join(main), Paths: nonFlags, Globs: flagValues(args, "-g", "--glob", "--iglob")}
		if len(nonFlags) > 0 {
			p.Path = shortDisplayPath(nonFlags[0])
		}

		return []Parsed{p}
	}

	return []Parsed{search(main, nonFlags)}
}

// search is a search for the first operand in the ones after it.
func search(main, operands []string) Parsed {
	p := Parsed{Kind: Search, Cmd: join(main)}
	if len(operands) > 0 {
		p.Query, p.Paths = operands[0], operands[1:]
	}
	if len(operands) > 1 {
		p.Path = shortDisplayPath(operands[1])
	}

	return p
}

func summarizeGit(main, tail []string) []Parsed {
	if len(tail) == 0 {
		return []Parsed{unknown(main)}
	}
	switch tail[0] {
	case "grep":
		return parseGrepLike(main, tail[1:])
	case "ls-files":
		return listWith([]string{flagExclude, "--exclude-from", "--pathspec-from-file"})(main, tail[1:])
	}

	return []Parsed{unknown(main)}
}

func summarizeFd(main, tail []string) []Parsed {
	query, dir := fdQueryAndPath(tail)

	return []Parsed{queryOrList(main, query, dir)}
}

func summarizeFind(main, tail []string) []Parsed {
	query, dir := findQueryAndPath(tail)

	return []Parsed{queryOrList(main, query, dir)}
}

// queryOrList is a search when there is a query, else a listing.
func queryOrList(main []string, query, dir string) Parsed {
	p := Parsed{Kind: ListFiles, Cmd: join(main), Query: query}
	if query != "" {
		p.Kind = Search
	}
	if dir != "" {
		p.Path, p.Paths = shortDisplayPath(dir), []string{dir}
	}

	return p
}

func summarizeAg(main, tail []string) []Parsed {
	var nonFlags []string
	for _, a := range skipFlagValues(trimAtConnector(tail), []string{"-G", "-g", "--file-search-regex", "--ignore-dir", "--ignore-file", "--path-to-ignore"}) {
		if !strings.HasPrefix(a, "-") {
			nonFlags = append(nonFlags, a)
		}
	}

	return []Parsed{search(main, nonFlags)}
}

// summarizeCat reads cat's one file. uah also reads each of several files
// (cat a b), where Codex calls the command unknown.
func summarizeCat(main, tail []string) []Parsed {
	ops := positionalOperands(tail, nil)
	if len(ops) == 0 {
		return []Parsed{unknown(main)}
	}
	out := make([]Parsed, 0, len(ops))
	for _, op := range ops {
		out = append(out, read(main, op))
	}

	return out
}

func summarizeHead(main, tail []string) []Parsed {
	if p, ok := headTailRead(main, tail, digits); ok {
		p.Lines = headLines(tail)

		return []Parsed{p}
	}

	return []Parsed{unknown(main)}
}

func summarizeTail(main, tail []string) []Parsed {
	if p, ok := headTailRead(main, tail, func(s string) bool { return digits(strings.TrimPrefix(s, "+")) }); ok {
		return []Parsed{p}
	}

	return []Parsed{unknown(main)}
}

// headTailRead reads the file of head -n N file, head -nN file, or head
// file (tail's the same, with +N counts).
func headTailRead(main, tail []string, count func(string) bool) (Parsed, bool) {
	validN := len(tail) > 0 && (tail[0] == "-n" && len(tail) > 1 && count(tail[1]) ||
		tail[0] != "-n" && strings.HasPrefix(tail[0], "-n") && count(tail[0][2:]))
	if validN {
		candidates := tail
		if tail[0] == "-n" && len(tail) > 1 && count(tail[1]) {
			candidates = tail[2:]
		}
		for _, c := range candidates {
			if !strings.HasPrefix(c, "-") {
				return read(main, c), true
			}
		}
	}
	if len(tail) == 1 && !strings.HasPrefix(tail[0], "-") {
		return read(main, tail[0]), true
	}

	return Parsed{}, false
}

func summarizeAwk(main, tail []string) []Parsed {
	if op, ok := awkDataFileOperand(tail); ok {
		return []Parsed{read(main, op)}
	}

	return []Parsed{unknown(main)}
}

func summarizeNl(main, tail []string) []Parsed {
	for _, c := range skipFlagValues(tail, []string{"-s", "-w", "-v", "-i", "-b"}) {
		if !strings.HasPrefix(c, "-") {
			return []Parsed{read(main, c)}
		}
	}

	return []Parsed{unknown(main)}
}

func summarizeSed(main, tail []string) []Parsed {
	if p, ok := sedReadPath(tail); ok {
		r := read(main, p)
		r.Lines = sedLines(tail)

		return []Parsed{r}
	}

	return []Parsed{unknown(main)}
}

// digits reports a non-empty run of ASCII digits.
func digits(s string) bool {
	if s == "" {
		return false
	}
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}

	return true
}

func contains(tokens []string, t string) bool { return slices.Contains(tokens, t) }
