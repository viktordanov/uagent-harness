// Adapted from openai/codex rust-v0.159.1 (Apache License 2.0, Copyright
// 2025 OpenAI): the tests in codex-rs/shell-command/src/parse_command.rs,
// without the PowerShell ones.

package cmdparse //nolint:testpackage // the formatting helpers are tested directly, as in Codex

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func splitSafe(s string) []string {
	if words, ok := split(s); ok {
		return words
	}

	return strings.Fields(s)
}

func bashLC(script string) []string { return []string{"bash", "-lc", script} }

func unk(cmd string) Parsed { return Parsed{Kind: Unknown, Cmd: cmd} }

func rd(cmd, name, p string) Parsed { return Parsed{Kind: Read, Cmd: cmd, Name: name, Path: p} }

func ls(cmd, p string) Parsed { return Parsed{Kind: ListFiles, Cmd: cmd, Path: p} }

func sr(cmd, query, p string) Parsed { return Parsed{Kind: Search, Cmd: cmd, Query: query, Path: p} }

// codexFields keeps the fields Codex's ParsedCommand has.
func codexFields(ps []Parsed) []Parsed {
	out := make([]Parsed, len(ps))
	for i, p := range ps {
		out[i] = Parsed{Kind: p.Kind, Cmd: p.Cmd, Name: p.Name, Path: p.Path, Query: p.Query}
	}

	return out
}

func TestParse_Codex(t *testing.T) {
	cases := []struct {
		name string
		argv []string
		want []Parsed
	}{
		{"git_status_is_unknown", []string{"git", "status"}, []Parsed{unk("git status")}},
		{"git_grep", splitSafe("git grep TODO src"), []Parsed{sr("git grep TODO src", "TODO", "src")}},
		{"git_grep_l", splitSafe("git grep -l TODO src"), []Parsed{sr("git grep -l TODO src", "TODO", "src")}},
		{"git_ls_files", splitSafe("git ls-files"), []Parsed{ls("git ls-files", "")}},
		{"git_ls_files_src", splitSafe("git ls-files src"), []Parsed{ls("git ls-files src", "src")}},
		{"git_ls_files_exclude", splitSafe("git ls-files --exclude target src"), []Parsed{ls("git ls-files --exclude target src", "src")}},
		{"handles_git_pipe_wc", bashLC("git status | wc -l"), []Parsed{unk("git status | wc -l")}},
		{"bash_lc_redirect_not_quoted", bashLC("echo foo > bar"), []Parsed{unk("echo foo > bar")}},
		{
			"handles_complex_bash_command_head",
			bashLC("rg --version && node -v && pnpm -v && rg --files | wc -l && rg --files | head -n 40"),
			[]Parsed{unk("rg --version && node -v && pnpm -v && rg --files | wc -l && rg --files | head -n 40")},
		},
		{"supports_searching_for_navigate_to_route", bashLC(`rg -n "navigate-to-route" -S`), []Parsed{sr("rg -n navigate-to-route -S", "navigate-to-route", "")}},
		{"handles_complex_bash_command", bashLC(`rg -n "BUG|FIXME|TODO|XXX|HACK" -S | head -n 200`), []Parsed{sr("rg -n 'BUG|FIXME|TODO|XXX|HACK' -S", "BUG|FIXME|TODO|XXX|HACK", "")}},
		{"supports_rg_files_with_path_and_pipe", bashLC("rg --files webview/src | sed -n"), []Parsed{ls("rg --files webview/src", "webview")}},
		{"supports_rg_files_then_head", bashLC("rg --files | head -n 50"), []Parsed{ls("rg --files", "")}},
		{
			"keeps_mutating_xargs_pipeline",
			bashLC(`rg -l QkBindingController presentation/src/main/java | xargs perl -pi -e 's/QkBindingController/QkController/g'`),
			[]Parsed{unk(`rg -l QkBindingController presentation/src/main/java | xargs perl -pi -e 's/QkBindingController/QkController/g'`)},
		},
		{
			"collapses_plain_pipeline_when_any_stage_is_unknown",
			splitSafe("rg -l QkBindingController presentation/src/main/java | xargs perl -pi -e 's/QkBindingController/QkController/g'"),
			[]Parsed{unk(join(splitSafe("rg -l QkBindingController presentation/src/main/java | xargs perl -pi -e 's/QkBindingController/QkController/g'")))},
		},
		{"collapses_pipeline_with_helper_when_later_stage_is_unknown", splitSafe("rg --files | nl -ba | foo"), []Parsed{unk(join(splitSafe("rg --files | nl -ba | foo")))}},
		{"rg_l", splitSafe("rg -l TODO src"), []Parsed{sr("rg -l TODO src", "TODO", "src")}},
		{"rg_files_with_matches", splitSafe("rg --files-with-matches TODO src"), []Parsed{sr("rg --files-with-matches TODO src", "TODO", "src")}},
		{"rg_L", splitSafe("rg -L TODO src"), []Parsed{sr("rg -L TODO src", "TODO", "src")}},
		{"rg_files_without_match", splitSafe("rg --files-without-match TODO src"), []Parsed{sr("rg --files-without-match TODO src", "TODO", "src")}},
		{"rga_l", splitSafe("rga -l TODO src"), []Parsed{sr("rga -l TODO src", "TODO", "src")}},
		{"supports_cat", bashLC("cat webview/README.md"), []Parsed{rd("cat webview/README.md", "README.md", "webview/README.md")}},
		{"zsh_lc_supports_cat", []string{"zsh", "-lc", "cat README.md"}, []Parsed{rd("cat README.md", "README.md", "README.md")}},
		{"supports_bat", bashLC("bat --theme TwoDark README.md"), []Parsed{rd("bat --theme TwoDark README.md", "README.md", "README.md")}},
		{"supports_batcat", bashLC("batcat README.md"), []Parsed{rd("batcat README.md", "README.md", "README.md")}},
		{"supports_less", bashLC("less -p TODO README.md"), []Parsed{rd("less -p TODO README.md", "README.md", "README.md")}},
		{"supports_more", bashLC("more README.md"), []Parsed{rd("more README.md", "README.md", "README.md")}},
		{"cd_then_cat_is_single_read", splitSafe("cd foo && cat foo.txt"), []Parsed{rd("cat foo.txt", "foo.txt", "foo/foo.txt")}},
		{"cd_with_double_dash_then_cat_is_read", splitSafe("cd -- -weird && cat foo.txt"), []Parsed{rd("cat foo.txt", "foo.txt", "-weird/foo.txt")}},
		{"cd_with_multiple_operands_uses_last", splitSafe("cd dir1 dir2 && cat foo.txt"), []Parsed{rd("cat foo.txt", "foo.txt", "dir2/foo.txt")}},
		{"bash_cd_then_bar_is_same_as_bar", splitSafe("bash -lc 'cd foo && bar'"), []Parsed{unk("cd foo && bar")}},
		{"bash_cd_then_cat_is_read", splitSafe("bash -lc 'cd foo && cat foo.txt'"), []Parsed{rd("cat foo.txt", "foo.txt", "foo/foo.txt")}},
		{"supports_ls_with_pipe", bashLC("ls -la | sed -n '1,120p'"), []Parsed{ls("ls -la", "")}},
		{"eza", splitSafe("eza --color=always src"), []Parsed{ls("eza '--color=always' src", "src")}},
		{"exa", splitSafe("exa -I target ."), []Parsed{ls("exa -I target .", ".")}},
		{"tree", splitSafe("tree -L 2 src"), []Parsed{ls("tree -L 2 src", "src")}},
		{"du", splitSafe("du -d 2 ."), []Parsed{ls("du -d 2 .", ".")}},
		{"supports_head_n", bashLC("head -n 50 Cargo.toml"), []Parsed{rd("head -n 50 Cargo.toml", "Cargo.toml", "Cargo.toml")}},
		{"supports_head_file_only", bashLC("head Cargo.toml"), []Parsed{rd("head Cargo.toml", "Cargo.toml", "Cargo.toml")}},
		{"supports_cat_sed_n", bashLC("cat tui/Cargo.toml | sed -n '1,200p'"), []Parsed{rd("cat tui/Cargo.toml | sed -n '1,200p'", "Cargo.toml", "tui/Cargo.toml")}},
		{"supports_tail_n_plus", bashLC("tail -n +522 README.md"), []Parsed{rd("tail -n +522 README.md", "README.md", "README.md")}},
		{"supports_tail_n_last_lines", bashLC("tail -n 30 README.md"), []Parsed{rd("tail -n 30 README.md", "README.md", "README.md")}},
		{"supports_tail_file_only", bashLC("tail README.md"), []Parsed{rd("tail README.md", "README.md", "README.md")}},
		{"supports_npm_run_build_is_unknown", []string{"npm", "run", "build"}, []Parsed{unk("npm run build")}},
		{"supports_grep_recursive_current_dir", []string{"grep", "-R", "CODEX_SANDBOX_ENV_VAR", "-n", "."}, []Parsed{sr("grep -R CODEX_SANDBOX_ENV_VAR -n .", "CODEX_SANDBOX_ENV_VAR", ".")}},
		{
			"supports_grep_recursive_specific_file",
			[]string{"grep", "-R", "CODEX_SANDBOX_ENV_VAR", "-n", "core/src/spawn.rs"},
			[]Parsed{sr("grep -R CODEX_SANDBOX_ENV_VAR -n core/src/spawn.rs", "CODEX_SANDBOX_ENV_VAR", "spawn.rs")},
		},
		{"egrep", splitSafe("egrep -R TODO src"), []Parsed{sr("egrep -R TODO src", "TODO", "src")}},
		{"fgrep", splitSafe("fgrep -l TODO src"), []Parsed{sr("fgrep -l TODO src", "TODO", "src")}},
		{"grep_l", splitSafe("grep -l TODO src"), []Parsed{sr("grep -l TODO src", "TODO", "src")}},
		{"grep_files_with_matches", splitSafe("grep --files-with-matches TODO src"), []Parsed{sr("grep --files-with-matches TODO src", "TODO", "src")}},
		{"grep_L", splitSafe("grep -L TODO src"), []Parsed{sr("grep -L TODO src", "TODO", "src")}},
		{"grep_files_without_match", splitSafe("grep --files-without-match TODO src"), []Parsed{sr("grep --files-without-match TODO src", "TODO", "src")}},
		{"supports_grep_query_with_slashes_not_shortened", splitSafe("grep -R src/main.rs -n ."), []Parsed{sr("grep -R src/main.rs -n .", "src/main.rs", ".")}},
		{"supports_grep_weird_backtick_in_query", splitSafe("grep -R COD`EX_SANDBOX -n"), []Parsed{sr("grep -R 'COD`EX_SANDBOX' -n", "COD`EX_SANDBOX", "")}},
		{"supports_cd_and_rg_files", splitSafe("cd codex-rs && rg --files"), []Parsed{ls("rg --files", "")}},
		{
			"supports_single_string_script_with_cd_and_pipe",
			bashLC(`cd /Users/pakrym/code/codex && rg -n "codex_api" codex-rs -S | head -n 50`),
			[]Parsed{sr("rg -n codex_api codex-rs -S", "codex_api", "codex-rs")},
		},
		{"supports_python_walks_files", bashLC(`python -c "import os; print(os.listdir('.'))"`), []Parsed{ls(join(splitSafe(`python -c "import os; print(os.listdir('.'))"`)), "")}},
		{"supports_python3_walks_files", bashLC(`python3 -c "import glob; print(glob.glob('*.rs'))"`), []Parsed{ls(join(splitSafe(`python3 -c "import glob; print(glob.glob('*.rs'))"`)), "")}},
		{"python_without_file_walk_is_unknown", bashLC(`python -c "print('hello')"`), []Parsed{unk(join(splitSafe(`python -c "print('hello')"`)))}},
		{"keeps_mutating_sed_1", bashLC("cat README.md && sed -n -i.bak 1p secret.txt"), []Parsed{unk("cat README.md && sed -n -i.bak 1p secret.txt")}},
		{"keeps_mutating_sed_2", bashLC("cat README.md && sed -ni.bak 1p secret.txt"), []Parsed{unk("cat README.md && sed -ni.bak 1p secret.txt")}},
		{"keeps_mutating_sed_3", bashLC("cat README.md && sed -Eni.bak 1p secret.txt"), []Parsed{unk("cat README.md && sed -Eni.bak 1p secret.txt")}},
		{"ignores_sed_operands_after_double_dash_when_checking_mutation", bashLC("cat README.md && sed 's/a/x/' -- -input.txt"), []Parsed{rd("cat README.md", "README.md", "README.md")}},
		{"supports_nl_then_sed_reading", bashLC("nl -ba core/src/parse_command.rs | sed -n '1200,1720p'"), []Parsed{rd("nl -ba core/src/parse_command.rs | sed -n '1200,1720p'", "parse_command.rs", "core/src/parse_command.rs")}},
		{"supports_sed_n", bashLC("sed -n '2000,2200p' tui/src/history_cell.rs"), []Parsed{rd("sed -n '2000,2200p' tui/src/history_cell.rs", "history_cell.rs", "tui/src/history_cell.rs")}},
		{"supports_awk_with_file", bashLC("awk '{print $1}' Cargo.toml"), []Parsed{rd("awk '{print $1}' Cargo.toml", "Cargo.toml", "Cargo.toml")}},
		{"filters_out_printf", bashLC(`printf "\n===== ansi-escape/Cargo.toml =====\n"; cat -- ansi-escape/Cargo.toml`), []Parsed{rd("cat -- ansi-escape/Cargo.toml", "Cargo.toml", "ansi-escape/Cargo.toml")}},
		{"drops_yes_in_pipelines", bashLC("yes | rg --files"), []Parsed{ls("rg --files", "")}},
		{
			"supports_sed_n_then_nl_as_search",
			splitSafe("sed -n '260,640p' exec/src/event_processor_with_human_output.rs | nl -ba"),
			[]Parsed{rd("sed -n '260,640p' exec/src/event_processor_with_human_output.rs", "event_processor_with_human_output.rs", "exec/src/event_processor_with_human_output.rs")},
		},
		{"preserves_rg_with_spaces", splitSafe("yes | rg -n 'foo bar' -S"), []Parsed{sr("rg -n 'foo bar' -S", "foo bar", "")}},
		{"ls_with_glob", splitSafe("ls -I '*.test.js'"), []Parsed{ls("ls -I '*.test.js'", "")}},
		{"strips_true_in_sequence_1", splitSafe("true && rg --files"), []Parsed{ls("rg --files", "")}},
		{"strips_true_in_sequence_2", splitSafe("rg --files && true"), []Parsed{ls("rg --files", "")}},
		{"strips_true_inside_bash_lc_1", bashLC("true && rg --files"), []Parsed{ls("rg --files", "")}},
		{"strips_true_inside_bash_lc_2", bashLC("rg --files || true"), []Parsed{ls("rg --files", "")}},
		{"shorten_path_on_windows", splitSafe(`cat "pkg\src\main.rs"`), []Parsed{rd(`cat "pkg\\src\\main.rs"`, "main.rs", `pkg\src\main.rs`)}},
		{"head_with_no_space", splitSafe("bash -lc 'head -n50 Cargo.toml'"), []Parsed{rd("head -n50 Cargo.toml", "Cargo.toml", "Cargo.toml")}},
		{"bash_dash_c_pipeline_parsing", []string{"bash", "-c", "rg --files | head -n 1"}, []Parsed{ls("rg --files", "")}},
		{"tail_with_no_space", splitSafe("bash -lc 'tail -n+10 README.md'"), []Parsed{rd("tail -n+10 README.md", "README.md", "README.md")}},
		{"grep_with_query_and_path", splitSafe("grep -R TODO src"), []Parsed{sr("grep -R TODO src", "TODO", "src")}},
		{"ag", splitSafe("ag TODO src"), []Parsed{sr("ag TODO src", "TODO", "src")}},
		{"ack", splitSafe("ack TODO src"), []Parsed{sr("ack TODO src", "TODO", "src")}},
		{"pt", splitSafe("pt TODO src"), []Parsed{sr("pt TODO src", "TODO", "src")}},
		{"rga", splitSafe("rga TODO src"), []Parsed{sr("rga TODO src", "TODO", "src")}},
		{"ag_l", splitSafe("ag -l TODO src"), []Parsed{sr("ag -l TODO src", "TODO", "src")}},
		{"ack_l", splitSafe("ack -l TODO src"), []Parsed{sr("ack -l TODO src", "TODO", "src")}},
		{"pt_l", splitSafe("pt -l TODO src"), []Parsed{sr("pt -l TODO src", "TODO", "src")}},
		{"rg_with_equals_style_flags", splitSafe("rg --colors=never -n foo src"), []Parsed{sr("rg '--colors=never' -n foo src", "foo", "src")}},
		{"cat_with_double_dash", splitSafe("cat -- ./-strange-file-name"), []Parsed{rd("cat -- ./-strange-file-name", "-strange-file-name", "./-strange-file-name")}},
		{"sed_ranges", splitSafe("sed -n '12,20p' Cargo.toml"), []Parsed{rd("sed -n '12,20p' Cargo.toml", "Cargo.toml", "Cargo.toml")}},
		{"drop_trailing_nl_in_pipeline", splitSafe("rg --files | nl -ba"), []Parsed{ls("rg --files", "")}},
		{"ls_with_time_style_and_path", splitSafe("ls --time-style=long-iso ./dist"), []Parsed{ls("ls '--time-style=long-iso' ./dist", ".")}},
		{"fd_type_path", splitSafe("fd -t f src/"), []Parsed{ls("fd -t f src/", "src")}},
		{"fd_query_path", splitSafe("fd main src"), []Parsed{sr("fd main src", "main", "src")}},
		{"find_basic_name_filter", splitSafe("find . -name '*.rs'"), []Parsed{sr("find . -name '*.rs'", "*.rs", ".")}},
		{"find_type_only_path", splitSafe("find src -type f"), []Parsed{ls("find src -type f", "src")}},
		{"bin_bash_lc_sed", splitSafe("/bin/bash -lc 'sed -n '1,10p' Cargo.toml'"), []Parsed{rd("sed -n '1,10p' Cargo.toml", "Cargo.toml", "Cargo.toml")}},
		{"bin_zsh_lc_sed", splitSafe("/bin/zsh -lc 'sed -n '1,10p' Cargo.toml'"), []Parsed{rd("sed -n '1,10p' Cargo.toml", "Cargo.toml", "Cargo.toml")}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, codexFields(Parse(tc.argv)))
		})
	}
}

func TestIsSmallFormattingCommand_Codex(t *testing.T) {
	for _, cmd := range []string{"wc", "tr", "cut", "sort", "uniq", "xargs", "tee", "column"} {
		assert.True(t, isSmallFormattingCommand(splitSafe(cmd)), cmd)
		assert.True(t, isSmallFormattingCommand(splitSafe(cmd+" -x")), cmd)
	}
	yes := []string{
		"awk '{print $1}'", "head", "head -n 40", "tail", "tail -n +10", "tail -n 30", "tail -c 30", "tail -c +10",
		"sed", "sed -n 10p", "sed -n p file.txt", "sed -n +10p file.txt",
	}
	no := []string{
		"awk '{print $1}' Cargo.toml", "awk -f script.awk Cargo.toml", "head -n 40 file.txt", "head file.txt",
		"tail -n +10 file.txt", "tail -n 30 file.txt", "tail file.txt",
		"sed -n 10p file.txt", "sed -n -e 10p file.txt", "sed -n 10p -- file.txt", "sed -n 1,200p file.txt",
	}
	for _, cmd := range yes {
		assert.True(t, isSmallFormattingCommand(splitSafe(cmd)), cmd)
	}
	for _, cmd := range no {
		assert.False(t, isSmallFormattingCommand(splitSafe(cmd)), cmd)
	}
	assert.False(t, isSmallFormattingCommand(nil))
}
