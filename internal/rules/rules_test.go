package rules_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent-harness/internal/rules"
)

const sample = `
prefix_rule(
    pattern = ["git", ["push", "fetch"]],
    decision = "prompt",
    justification = "Pushing affects the remote",
    match = [["git", "push", "origin"], "git fetch"],
    not_match = ["git status"],
)
prefix_rule(pattern = ["git", "push", "--force"], decision = "forbidden", justification = "Use --force-with-lease.")
prefix_rule(pattern = ["go", "test"])
prefix_rule(pattern = ["ls"], decision = "allow")
host_executable(name = "git", paths = ["/usr/bin/git"])
network_rule(host = "pypi.org", protocol = "https", decision = "allow")
`

func TestParse(t *testing.T) {
	got, err := rules.Parse("sample.rules", []byte(sample))
	require.NoError(t, err)
	require.Len(t, got, 4)
	assert.Equal(t, [][]string{{"git"}, {"push", "fetch"}}, got[0].Pattern)
	assert.Equal(t, rules.Prompt, got[0].Decision)
	assert.Equal(t, "Pushing affects the remote", got[0].Justification)
	assert.Equal(t, rules.Allow, got[2].Decision, "allow is the default")
	assert.Equal(t, "sample.rules", got[2].Source)
	assert.Equal(t, `prefix_rule(pattern=["git", ["push", "fetch"]], decision="prompt", justification="Pushing affects the remote")`, got[0].String())

	for name, src := range map[string]string{
		"bad decision":        `prefix_rule(pattern=["a"], decision="deny")`,
		"empty pattern":       `prefix_rule(pattern=[])`,
		"a failing match":     `prefix_rule(pattern=["a", "b"], match=["a c"])`,
		"a failing not_match": `prefix_rule(pattern=["a"], not_match=[["a", "b"]])`,
		"empty justification": `prefix_rule(pattern=["a"], justification=" ")`,
		"unknown argument":    `prefix_rule(pattern=["a"], reason="x")`,
		"syntax error":        `prefix_rule(`,
		"unknown function":    `exec_rule(pattern=["a"])`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := rules.Parse("bad.rules", []byte(src))
			assert.Error(t, err)
		})
	}
}

func TestSplit(t *testing.T) {
	for _, tc := range []struct {
		command string
		want    [][]string
		ok      bool
	}{
		{"git status", [][]string{{"git", "status"}}, true},
		{`git commit -m "a message" && git push`, [][]string{{"git", "commit", "-m", "a message"}, {"git", "push"}}, true},
		{"ls; cat 'a b' | wc -l || true", [][]string{{"ls"}, {"cat", "a b"}, {"wc", "-l"}, {"true"}}, true},
		{`echo a\ b`, [][]string{{"echo", "a b"}}, true},
		{"echo hi > out.txt", nil, false},
		{"echo $HOME", nil, false},
		{"echo $(date)", nil, false},
		{"(cd x && ls)", nil, false},
		{"if true; then ls; fi", nil, false},
		{"FOO=1 go test", nil, false},
		{"rm *.txt", nil, false},
		{"sleep 1 &", nil, false},
		{"", nil, false},
		{"echo 'unterminated", nil, false},
	} {
		t.Run(tc.command, func(t *testing.T) {
			got, ok := rules.Split(tc.command)
			assert.Equal(t, tc.ok, ok)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestCheck(t *testing.T) {
	parsed, err := rules.Parse("sample.rules", []byte(sample))
	require.NoError(t, err)
	p := rules.New(parsed...)
	for _, tc := range []struct {
		command string
		want    rules.Decision // 0: no decision
	}{
		{"git status", 0},
		{"git push origin", rules.Prompt},
		{"git push --force origin", rules.Forbidden},
		{"go test ./...", rules.Allow},
		{"/usr/local/go/bin/go test ./...", rules.Allow},
		{"go vet ./...", 0},
		{"go test ./... && ls", rules.Allow},
		{"go test ./... && rm -rf x", 0},
		{"ls && git push --force", rules.Forbidden},
		{"ls && git fetch", rules.Prompt},
		{"ls > x", 0},
	} {
		t.Run(tc.command, func(t *testing.T) {
			cmds, _ := rules.Split(tc.command)
			r, ok := p.Check(cmds)
			assert.Equal(t, tc.want != 0, ok)
			assert.Equal(t, tc.want, r.Decision)
		})
	}
}

func TestLoadAndAppend(t *testing.T) {
	user, project := t.TempDir(), t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(user, "a.rules"), []byte(`prefix_rule(pattern=["ls"])`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(project, "b.rules"), []byte(`prefix_rule(pattern=["rm"], decision="forbidden")`+"\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(project, "notes.txt"), []byte("not rules"), 0o600))

	path := filepath.Join(user, rules.DefaultFile)
	_, err := rules.AppendAllow(filepath.Join(user, "a.rules"), []string{"git", "pull"})
	require.NoError(t, err)
	r, err := rules.AppendAllow(path, []string{"cargo", "build"})
	require.NoError(t, err)
	assert.Equal(t, rules.Allow, r.Decision)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, `prefix_rule(pattern=["cargo", "build"], decision="allow")`+"\n", string(data))
	data, err = os.ReadFile(filepath.Join(user, "a.rules"))
	require.NoError(t, err)
	assert.Equal(t, `prefix_rule(pattern=["ls"])`+"\n"+`prefix_rule(pattern=["git", "pull"], decision="allow")`+"\n", string(data))

	loaded, err := rules.LoadDirs(user, project, filepath.Join(project, "missing"))
	require.NoError(t, err)
	p := rules.New(loaded...)
	for cmd, want := range map[string]rules.Decision{"ls": rules.Allow, "git pull": rules.Allow, "cargo build --release": rules.Allow, "rm x": rules.Forbidden} {
		m, ok := p.Match(mustWords(t, cmd))
		assert.True(t, ok, cmd)
		assert.Equal(t, want, m.Decision, cmd)
	}

	require.NoError(t, os.WriteFile(filepath.Join(project, "c.rules"), []byte(`prefix_rule(pattern=1)`), 0o600))
	_, err = rules.LoadDirs(project)
	require.ErrorContains(t, err, "c.rules")
}

func TestFromPrefixes(t *testing.T) {
	got, err := rules.FromPrefixes([]string{"git status", "go test"}, rules.Allow, "config.toml")
	require.NoError(t, err)
	assert.Equal(t, [][]string{{"git"}, {"status"}}, got[0].Pattern)
	assert.Equal(t, "config.toml", got[1].Source)

	_, err = rules.FromPrefixes([]string{"echo $HOME"}, rules.Forbidden, "config.toml")
	require.Error(t, err)
}

func mustWords(t *testing.T, s string) []string {
	t.Helper()
	w, ok := rules.Words(s)
	require.True(t, ok)

	return w
}
