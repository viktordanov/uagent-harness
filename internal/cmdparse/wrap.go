package cmdparse

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// uah's own steps, before and after Codex's parse: a command arrives
// wrapped (rtk proxy sh -c '…'), with absolute paths, and sometimes with a
// heredoc, none of which says what it does. These steps are not Codex's.

// Env is where a command runs, for showing its paths: under the workspace
// relative to it, under the home directory after ~.
type Env struct {
	Workspace, Home string
}

// rtkCommands are rtk's own commands, which are not a wrapper around
// another command.
var rtkCommands = map[string]bool{"gain": true, "discover": true, "init": true, "config": true, "help": true, "--version": true, "-V": true, "--help": true}

// Strip removes the wrappers around a command, repeatedly: rtk proxy and
// rtk (a token-saving proxy that runs the command after it), and a shell
// that runs one quoted script (sh -c '…', bash -lc '…', zsh -lc '…').
func Strip(command string) string {
	s := strings.TrimSpace(command)
	for {
		next, ok := stripOnce(s)
		if !ok {
			return s
		}
		s = next
	}
}

func stripOnce(s string) (string, bool) {
	first, rest, _ := strings.Cut(s, " ")
	rest = strings.TrimLeft(rest, " ")
	if first == "rtk" && rest != "" {
		second, after, _ := strings.Cut(rest, " ")
		if second == "proxy" {
			return strings.TrimLeft(after, " "), after != ""
		}

		return rest, !rtkCommands[second]
	}
	switch filepath.Base(first) {
	case "sh", shBash, shZsh:
	default:
		return s, false
	}
	flag, script, _ := strings.Cut(rest, " ")
	if flag != "-c" && flag != flagLC && flag != "-cl" {
		return s, false
	}
	script = strings.TrimSpace(script)
	if script == "" || (script[0] != '\'' && script[0] != '"') {
		return s, false
	}
	words, ok := split(script)
	if !ok || len(words) != 1 {
		return s, false
	}

	return strings.TrimSpace(words[0]), true
}

// Relative shows the paths in text under the workspace relative to it
// ("." for the workspace itself) and those under the home directory after
// ~. A path counts only where a word or a quoted string starts. git -C .
// then says nothing and goes.
func Relative(text string, env Env) string {
	if env.Workspace != "" {
		text = replacePath(text, filepath.Clean(env.Workspace), ".")
	}
	if env.Home != "" {
		text = replacePath(text, filepath.Clean(env.Home), "~")
	}

	return strings.ReplaceAll(text, "git -C . ", "git ")
}

// replacePath replaces dir, and dir/ before a path, with short.
func replacePath(text, dir, short string) string {
	if dir == "/" || dir == "." {
		return text
	}
	var b strings.Builder
	for {
		i := strings.Index(text, dir)
		if i < 0 {
			b.WriteString(text)

			return b.String()
		}
		end := i + len(dir)
		starts := i == 0 || strings.IndexByte(" \t\n'\"=(:", text[i-1]) >= 0
		switch {
		case starts && end < len(text) && text[end] == '/':
			b.WriteString(text[:i])
			if short == "." {
				end++ // a/b, not ./a/b
			} else {
				b.WriteString(short)
			}
		case starts && (end == len(text) || strings.IndexByte(" \t\n'\";:)", text[end]) >= 0):
			b.WriteString(text[:i] + short)
		default:
			b.WriteString(text[:end])
		}
		text = text[end:]
	}
}

// heredocStart finds a heredoc's operator: <<, <<-, and a delimiter,
// bare or quoted.
var heredocStart = regexp.MustCompile(`\s*<<(-?)\s*(['"]?)([A-Za-z_][A-Za-z0-9_]*)(['"]?)`)

// heredoc folds a script whose first line feeds a heredoc to a command:
// the command with the heredoc as <<DELIM (without the - that asks the
// command to read standard input), and the body's size and meaningful
// lines, joined by "; ". ok is false for any other script.
func heredoc(script string) (head, body string, ok bool) {
	first, rest, multi := strings.Cut(script, "\n")
	m := heredocStart.FindStringSubmatchIndex(first)
	if !multi || m == nil {
		return "", "", false
	}
	delim := first[m[6]:m[7]]
	head = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(first[:m[0]]), " -"))
	head += " <<" + delim + first[m[1]:]
	var lines, meaningful []string
	for l := range strings.SplitSeq(rest, "\n") {
		if strings.TrimSpace(l) == delim {
			break
		}
		lines = append(lines, l)
		if t := strings.TrimSpace(l); t != "" && !strings.HasPrefix(t, "#") {
			meaningful = append(meaningful, t)
		}
	}
	noun := " lines"
	if len(lines) == 1 {
		noun = " line"
	}
	body = strconv.Itoa(len(lines)) + noun
	if len(meaningful) > 0 {
		body += " · " + strings.Join(meaningful, "; ")
	}

	return head, body, true
}
