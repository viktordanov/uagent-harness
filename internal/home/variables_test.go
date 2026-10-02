package home_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uah/internal/home"
)

// setForOthers are the variables uah sets for the commands it runs, and
// does not read.
var setForOthers = []string{"UAH_HOOK_EVENT", "UAH_PROJECT_DIR"}

// TestVariables: every uah or runner variable the product code names is in
// home.Variables (or is UAH_HOME, or one uah sets for hooks), and every
// listed variable is named in the product code.
func TestVariables(t *testing.T) {
	root := filepath.Join("..", "..")
	name := regexp.MustCompile(`"((?:UAH|UAGENT|UNREAL_HARNESS)_[A-Z0-9_]+|CODEX_REFRESH_TOKEN_URL_OVERRIDE)"`)
	named := map[string]bool{}
	for _, dir := range []string{"cmd", "internal"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			src, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, m := range name.FindAllSubmatch(src, -1) {
				named[string(m[1])] = true
			}

			return nil
		})
		require.NoError(t, err)
	}

	for v := range named {
		if v != home.Env && !slices.Contains(setForOthers, v) {
			assert.Contains(t, home.Variables, v, "uah reads %s; add it to home.Variables", v)
		}
	}
	for _, v := range home.Variables {
		assert.True(t, named[v], "uah does not read %s; remove it from home.Variables", v)
	}
}
