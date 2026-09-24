package rules

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// DefaultFile is the rules file "don't ask again" appends to, as in Codex.
const DefaultFile = "default.rules"

// LoadDirs reads every *.rules file in the directories, in order and by
// name within a directory. A missing directory has no rules.
func LoadDirs(dirs ...string) ([]Rule, error) {
	var out []Rule
	for _, dir := range dirs {
		paths, err := filepath.Glob(filepath.Join(dir, "*.rules"))
		if err != nil {
			return nil, fmt.Errorf("failed to list rules in %s: %w", dir, err)
		}
		for _, path := range paths {
			src, err := os.ReadFile(path)
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			if err != nil {
				return nil, fmt.Errorf("failed to read %s: %w", path, err)
			}
			rules, err := Parse(path, src)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", path, err)
			}
			out = append(out, rules...)
		}
	}

	return out, nil
}

// AppendAllow adds `prefix_rule(pattern=[...], decision="allow")` to the
// file, creating it and its directory, and returns the rule.
func AppendAllow(path string, prefix []string) (Rule, error) {
	if len(prefix) == 0 {
		return Rule{}, errors.New("failed to add a rule: the prefix is empty")
	}
	pattern := make([][]string, 0, len(prefix))
	for _, w := range prefix {
		pattern = append(pattern, []string{w})
	}
	r := Rule{Pattern: pattern, Decision: Allow, Source: path}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return Rule{}, fmt.Errorf("failed to create the rules directory: %w", err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return Rule{}, fmt.Errorf("failed to open %s: %w", path, err)
	}
	line := r.String() + "\n"
	if info, err := f.Stat(); err == nil && info.Size() > 0 && !endsWithNewline(path, info.Size()) {
		line = "\n" + line
	}
	_, werr := f.WriteString(line)
	if err := errors.Join(werr, f.Close()); err != nil {
		return Rule{}, fmt.Errorf("failed to write %s: %w", path, err)
	}

	return r, nil
}

func endsWithNewline(path string, size int64) bool {
	f, err := os.Open(path)
	if err != nil {
		return true
	}
	defer f.Close()
	b := make([]byte, 1)
	if _, err := f.ReadAt(b, size-1); err != nil {
		return true
	}

	return b[0] == '\n'
}

// FromPrefixes turns configured command prefixes, such as "git status",
// into rules with the decision.
func FromPrefixes(prefixes []string, d Decision, source string) ([]Rule, error) {
	out := make([]Rule, 0, len(prefixes))
	for _, p := range prefixes {
		words, ok := Words(p)
		if !ok {
			return nil, fmt.Errorf("%s: %q is not a simple command prefix", source, p)
		}
		pattern := make([][]string, 0, len(words))
		for _, w := range words {
			pattern = append(pattern, []string{w})
		}
		out = append(out, Rule{Pattern: pattern, Decision: d, Source: source})
	}

	return out, nil
}
