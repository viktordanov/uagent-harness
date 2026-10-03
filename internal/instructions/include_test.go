package instructions_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uah/internal/instructions"
)

func TestAssemble_Includes(t *testing.T) {
	assemble := func(t *testing.T, paths ...string) (string, []instructions.File) {
		t.Helper()
		files := make([]instructions.File, 0, len(paths))
		for _, p := range paths {
			files = append(files, instructions.File{Path: p})
		}
		text, used, _, err := instructions.Assemble(files, 0)
		require.NoError(t, err)

		return text, used
	}

	t.Run("expands an @ line in place, relative to its file and from ~", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		dir := t.TempDir()
		agents := filepath.Join(dir, "AGENTS.md")
		write(t, agents, "Before.\n@RTK.md\n@~/global.md\nAfter, and @inline.md is not a line of its own.")
		write(t, filepath.Join(dir, "RTK.md"), "Prefix commands with rtk.\n@sub/deep.md")
		write(t, filepath.Join(dir, "sub", "deep.md"), "Deep rule.")
		write(t, filepath.Join(home, "global.md"), "Global rule.")

		text, used := assemble(t, agents)

		rtk, deep, global := filepath.Join(dir, "RTK.md"), filepath.Join(dir, "sub", "deep.md"), filepath.Join(home, "global.md")
		assert.Equal(t, fmt.Sprintf("## %s\n\nBefore.\n"+
			"<include path=%q>\nPrefix commands with rtk.\n<include path=%q>\nDeep rule.\n</include>\n</include>\n"+
			"<include path=%q>\nGlobal rule.\n</include>\n"+
			"After, and @inline.md is not a line of its own.\n", agents, rtk, deep, global), text)
		assert.Equal(t, []string{rtk, deep, global}, used[0].Includes)
	})

	t.Run("notes a missing file, a loop, a repeat, an instruction file, and a fenced line", func(t *testing.T) {
		dir := t.TempDir()
		agents, local := filepath.Join(dir, "AGENTS.md"), filepath.Join(dir, "sub", "AGENTS.md")
		write(t, agents, "@missing.md\n@a.md\n@a.md\n@sub/AGENTS.md\n```\n@a.md\n```")
		write(t, filepath.Join(dir, "a.md"), "A.\n@b.md")
		write(t, filepath.Join(dir, "b.md"), "B.\n@a.md")
		write(t, local, "Local.")

		text, used := assemble(t, agents, local)

		a := filepath.Join(dir, "a.md")
		assert.Contains(t, text, "@missing.md (uah could not include "+filepath.Join(dir, "missing.md")+": no such file or directory)")
		assert.Contains(t, text, "B.\n@a.md ("+a+" is included above)", "the loop ends")
		assert.Equal(t, 1, strings.Count(text, "<include path=\""+a+"\">"), "a file is expanded once")
		assert.Contains(t, text, "@sub/AGENTS.md ("+local+" is an instruction file of its own)")
		assert.Contains(t, text, "```\n@a.md\n```", "a fenced line stays as it is")
		assert.Contains(t, text, "## "+local+"\n\nLocal.")
		assert.Equal(t, []string{a, filepath.Join(dir, "b.md")}, used[0].Includes)
	})

	t.Run("stops nesting at the depth limit", func(t *testing.T) {
		dir := t.TempDir()
		agents := filepath.Join(dir, "AGENTS.md")
		write(t, agents, "@1.md")
		for i := 1; i <= instructions.MaxIncludeDepth+1; i++ {
			write(t, filepath.Join(dir, fmt.Sprintf("%d.md", i)), fmt.Sprintf("Level %d.\n@%d.md", i, i+1))
		}

		text, used := assemble(t, agents)

		assert.Len(t, used[0].Includes, instructions.MaxIncludeDepth)
		assert.Contains(t, text, fmt.Sprintf("@%d.md (not included: includes nest deeper than %d", instructions.MaxIncludeDepth+1, instructions.MaxIncludeDepth))
	})

	t.Run("cuts the includes at their cap", func(t *testing.T) {
		dir := t.TempDir()
		agents := filepath.Join(dir, "AGENTS.md")
		write(t, agents, "@big.md\n@small.md")
		write(t, filepath.Join(dir, "big.md"), strings.Repeat("line\n", instructions.MaxIncludeBytes/5+100))
		write(t, filepath.Join(dir, "small.md"), "Small.")

		text, used := assemble(t, agents)

		assert.Contains(t, text, "(cut: the included files passed 16384 bytes; read "+filepath.Join(dir, "big.md")+" for the rest)")
		assert.Contains(t, text, "@small.md (not included: the included files passed 16384 bytes; read "+filepath.Join(dir, "small.md")+" when you need it)")
		assert.Equal(t, []string{filepath.Join(dir, "big.md")}, used[0].Includes)
		assert.Less(t, len(text), instructions.MaxIncludeBytes+1024)
	})

	t.Run("a file with no @ lines is unchanged", func(t *testing.T) {
		dir := t.TempDir()
		agents := filepath.Join(dir, "AGENTS.md")
		require.NoError(t, os.WriteFile(agents, []byte("Email me @ noon.\n@ alone\n"), 0o600))

		text, used := assemble(t, agents)

		assert.Equal(t, "## "+agents+"\n\nEmail me @ noon.\n@ alone\n", text)
		assert.Empty(t, used[0].Includes)
	})
}
