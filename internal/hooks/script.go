package hooks

import (
	"os"
	"path/filepath"
	"strings"

	"mvdan.cc/sh/v3/expand"
	"mvdan.cc/sh/v3/syntax"
)

// scriptPath is the local file a hook command runs: its first word, when
// that word is a path (it has a slash, so the shell does not search PATH)
// to an existing regular file, absolute or relative to the workspace. The
// word may use environment variables, such as "$UAH_PROJECT_DIR"/hook.sh.
// It is "" when the command runs no local script.
func scriptPath(command, workspace string) string {
	file, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(command), "")
	if err != nil || len(file.Stmts) == 0 {
		return ""
	}
	call, ok := firstCall(file.Stmts[0].Cmd)
	if !ok || len(call.Args) == 0 {
		return ""
	}
	env := expand.FuncEnviron(func(name string) string {
		if name == "UAH_PROJECT_DIR" {
			return workspace
		}

		return os.Getenv(name)
	})
	word, err := expand.Literal(&expand.Config{Env: env}, call.Args[0])
	if err != nil || !strings.Contains(word, "/") {
		return ""
	}
	if rest, ok := strings.CutPrefix(word, "~/"); ok {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		word = filepath.Join(home, rest)
	}
	if !filepath.IsAbs(word) {
		word = filepath.Join(workspace, word)
	}
	if info, err := os.Stat(word); err != nil || !info.Mode().IsRegular() {
		return ""
	}

	return filepath.Clean(word)
}

// firstCall is the first simple command of a statement, looking into the
// left side of `&&`, `||`, and pipes.
func firstCall(cmd syntax.Command) (*syntax.CallExpr, bool) {
	switch c := cmd.(type) {
	case *syntax.CallExpr:
		return c, true
	case *syntax.BinaryCmd:
		return firstCall(c.X.Cmd)
	}

	return nil, false
}
