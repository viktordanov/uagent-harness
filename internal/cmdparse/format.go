// Adapted from openai/codex rust-v0.159.1 (Apache License 2.0, Copyright
// 2025 OpenAI): codex-rs/shell-command/src/parse_command.rs (sed reads
// and the formatting helpers a pipeline drops).

package cmdparse

import (
	"strings"
)

// isValidSedN reports a sed -n range script: 12p or 12,20p.
func isValidSedN(s string) bool {
	body, ok := strings.CutSuffix(s, "p")
	if !ok {
		return false
	}
	a, b, two := strings.Cut(body, ",")
	if !two {
		return digits(a)
	}

	return digits(a) && digits(b) && !strings.Contains(b, ",")
}

// sedReadPath is the file of sed -n <range> file; ok is false for any
// other sed, and for one that edits in place.
func sedReadPath(args []string) (string, bool) {
	args = trimAtConnector(args)
	if sedHasInPlaceFlag(args) || !contains(args, "-n") || !hasSedRange(args) {
		return "", false
	}
	var nonFlags []string
	for _, a := range skipFlagValues(args, []string{"-e", "-f", flagExpression, flagFile}) {
		if !strings.HasPrefix(a, "-") {
			nonFlags = append(nonFlags, a)
		}
	}
	switch {
	case len(nonFlags) == 0:
		return "", false
	case isValidSedN(nonFlags[0]):
		if len(nonFlags) > 1 {
			return nonFlags[1], true
		}

		return "", false
	}

	return nonFlags[0], true
}

// hasSedRange reports a range script given with -e or as an operand.
func hasSedRange(args []string) bool {
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-e", flagExpression:
			if i+1 < len(args) && isValidSedN(args[i+1]) {
				return true
			}
			i++
		case "-f", flagFile:
			i++
		}
	}
	for _, a := range args {
		if !strings.HasPrefix(a, "-") && isValidSedN(a) {
			return true
		}
	}

	return false
}

// sedLines is a sed read's range as "1-360" (uah).
func sedLines(args []string) string {
	for _, a := range args {
		if !strings.HasPrefix(a, "-") && isValidSedN(a) {
			return strings.Replace(strings.TrimSuffix(a, "p"), ",", "-", 1)
		}
	}

	return ""
}

// headLines is the range head -n N shows, as "1-N" (uah).
func headLines(args []string) string {
	n := ""
	switch {
	case len(args) > 1 && args[0] == "-n":
		n = args[1]
	case len(args) > 0 && strings.HasPrefix(args[0], "-n"):
		n = args[0][2:]
	case len(args) > 0 && strings.HasPrefix(args[0], "-"):
		n = args[0][1:]
	}
	if !digits(n) {
		return ""
	}

	return "1-" + n
}

// sedHasInPlaceFlag reports -i or --in-place, also inside grouped short
// flags such as -ni.bak.
func sedHasInPlaceFlag(tokens []string) bool {
	for i := 0; i < len(tokens); i++ {
		t := tokens[i]
		switch {
		case t == "--":
			return false
		case t == "-e" || t == "-f" || t == flagExpression || t == flagFile:
			i++
		case t == "--in-place" || strings.HasPrefix(t, "--in-place="):
			return true
		case strings.HasPrefix(t, "--"):
		case strings.HasPrefix(t, "-"):
			inPlace, takesNext := shortSedFlags(t[1:])
			if inPlace {
				return true
			}
			if takesNext {
				i++
			}
		}
	}

	return false
}

// shortSedFlags reads grouped short flags: i edits in place, and an e or
// f ends the group, taking the next word when it is the group's last.
func shortSedFlags(group string) (inPlace, takesNext bool) {
	for j, c := range group {
		switch c {
		case 'i':
			return true, false
		case 'e', 'f':
			return false, j == len(group)-1
		}
	}

	return false, false
}

// isSmallFormattingCommand reports a helper a pipeline uses to shape
// output (head -n 40, wc -l, awk '{…}'), which the summary drops in favor
// of the command that does the work. Variants with a file operand stay.
func isSmallFormattingCommand(tokens []string) bool {
	if len(tokens) == 0 {
		return false
	}
	switch tokens[0] {
	case "wc", "tr", "cut", "sort", "uniq", "tee", "column", "yes", "printf":
		return true
	case "xargs":
		return !isMutatingXargs(tokens)
	case "awk":
		_, ok := awkDataFileOperand(tokens[1:])

		return !ok
	case cmdHead:
		return formattingCount(tokens, false)
	case "tail":
		return formattingCount(tokens, true)
	case cmdSed:
		_, ok := sedReadPath(tokens[1:])

		return !sedHasInPlaceFlag(tokens[1:]) && !ok
	}

	return false
}

// formattingCount reports head or tail with no file: bare, one flag, or
// -n N (tail: also -c N and +N counts).
func formattingCount(tokens []string, isTail bool) bool {
	switch len(tokens) {
	case 1:
		return true
	case 2:
		return strings.HasPrefix(tokens[1], "-")
	case 3:
		flag, count := tokens[1], tokens[2]
		if !isTail {
			return (flag == "-n" || flag == "-c") && allDigits(count)
		}

		return (flag == "-n" || flag == "-c") && (allDigits(count) || strings.HasPrefix(count, "+") && allDigits(count[1:]))
	}

	return false
}

// allDigits is Rust's chars().all(is_ascii_digit): true for "".
func allDigits(s string) bool { return s == "" || digits(s) }

func isMutatingXargs(tokens []string) bool {
	sub := xargsSubcommand(tokens)
	if len(sub) == 0 {
		return false
	}
	switch sub[0] {
	case "perl", "ruby":
		return hasInPlaceFlag(sub[1:])
	case cmdSed:
		return sedHasInPlaceFlag(sub[1:])
	case "rg":
		return contains(sub[1:], "--replace")
	}

	return false
}

// xargsSubcommand is the command xargs runs, after its own flags.
func xargsSubcommand(tokens []string) []string {
	if len(tokens) == 0 || tokens[0] != "xargs" {
		return nil
	}
	for i := 1; i < len(tokens); {
		t := tokens[i]
		switch {
		case t == "--":
			return tokens[i+1:]
		case !strings.HasPrefix(t, "-"):
			return tokens[i:]
		case len(t) == 2 && strings.IndexByte("EeILnPs", t[1]) >= 0:
			i += 2
		default:
			i++
		}
	}

	return nil
}

func hasInPlaceFlag(tokens []string) bool {
	for _, t := range tokens {
		if strings.HasPrefix(t, "-i") || strings.HasPrefix(t, "-pi") || t == "--in-place" || strings.HasPrefix(t, "--in-place=") {
			return true
		}
	}

	return false
}
