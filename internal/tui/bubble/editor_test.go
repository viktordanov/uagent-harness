package bubble_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"mvdan.cc/sh/v3/syntax"

	"github.com/viktordanov/uah/internal/approval"
	"github.com/viktordanov/uah/internal/tui/bubble"
)

// The test binary is the editor: VISUAL names it, and TestMain sends it
// here when fakeEditorEnv is set. It records what it was given in the file
// fakeEditorSeenEnv names, then edits the file as fakeEditorEnv says.
const (
	fakeEditorEnv     = "UAH_TEST_FAKE_EDITOR"
	fakeEditorSeenEnv = "UAH_TEST_FAKE_EDITOR_SEEN"
)

// seen is what the fake editor was given.
type seen struct {
	Args []string
	Path string
	Text string
	Mode os.FileMode
}

func fakeEditor(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 2
	}
	info, err := os.Stat(path)
	if err != nil {
		return 2
	}
	record, _ := json.Marshal(seen{Args: os.Args[1:], Path: path, Text: string(data), Mode: info.Mode().Perm()})
	if err := os.WriteFile(os.Getenv(fakeEditorSeenEnv), record, 0o600); err != nil {
		return 2
	}
	text := string(data)
	switch os.Getenv(fakeEditorEnv) {
	case "keep": // saved as it is; vim ends the last line
		text += "\n"
	case "edit": // delete a placeholder, add lines, type an unknown placeholder
		text = strings.Replace(text, "[Image #1] ", "", 1) + "\nsecond line\nthird with [Image #9]\n"
	case "empty":
		text = ""
	case "fail":
		return 3
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		return 2
	}

	return 0
}

// fakeExec stands in for tea.Exec, which only a tea.Program runs: it holds
// the edit ctrl+g started until the test runs it.
type fakeExec struct {
	record string // what the editor saw
	home   string // uah's home, $UAH_HOME
	run    func() tea.Msg
}

// useFakeEditor makes the test binary the editor, with an argument as
// `code --wait` has, run without a terminal to release.
func useFakeEditor(t *testing.T, d *bubble.Deps, action string) *fakeExec {
	t.Helper()
	fe := &fakeExec{}
	exe, err := syntax.Quote(os.Args[0], syntax.LangPOSIX)
	require.NoError(t, err)
	record := filepath.Join(t.TempDir(), "seen.json")
	fe.home = filepath.Join(t.TempDir(), "home")
	t.Setenv("UAH_HOME", fe.home)
	t.Setenv("VISUAL", exe+" --wait")
	t.Setenv("EDITOR", "must-not-run")
	t.Setenv(fakeEditorEnv, action)
	t.Setenv(fakeEditorSeenEnv, record)
	fe.record = record
	d.Exec = func(c tea.ExecCommand, fn tea.ExecCallback) tea.Cmd {
		fe.run = func() tea.Msg { return fn(c.Run()) }

		return nil
	}

	return fe
}

// edit runs the edit ctrl+g started, as tea.Program would, and hands its
// result to the model.
func (d *driver) edit(fe *fakeExec) {
	d.t.Helper()
	require.NotNil(d.t, fe.run, "ctrl+g started no editor")
	run := fe.run
	fe.run = nil
	d.send(run())
}

func readSeen(t *testing.T, record string) seen {
	t.Helper()
	data, err := os.ReadFile(record)
	require.NoError(t, err)
	var s seen
	require.NoError(t, json.Unmarshal(data, &s))

	return s
}

func (d *driver) draft() string { return d.m.(bubble.Model).Draft() }

// TestTUI_EditTheDraftInTheEditor: ctrl+g writes the draft, placeholders as
// text, to a private .md file in <uah home>/editor, runs $VISUAL with its
// arguments, and the saved text is the draft: the image whose placeholder
// was deleted drops, the other goes with the message, and a placeholder
// typed in the editor is text.
func TestTUI_EditTheDraftInTheEditor(t *testing.T) {
	// Full access: the test's home is in the temporary directory, which the
	// sandbox writes (TestTUI_EditorRefusesAnExposedDraftDir).
	d, llm, workspace := imageDepsIn(t, approval.ModeYolo)
	fe := useFakeEditor(t, &d, "edit")
	dr := start(t, d)
	dr.until("the session is open", func() bool { return dr.m.(bubble.Model).Exit().SessionID != "" })
	dr.key('v', tea.ModCtrl)
	dr.waitFor("λ [Image #1]")
	dr.key('v', tea.ModCtrl)
	dr.waitFor("[Image #1] [Image #2]")
	dr.typeText("compare")

	dr.key('g', tea.ModCtrl)
	dr.edit(fe)
	want := "[Image #2] compare\nsecond line\nthird with [Image #9]"
	assert.Equal(t, want, dr.draft())
	assert.Equal(t, []string{"[Image #2]"}, dr.m.(bubble.Model).Attached())

	got := readSeen(t, fe.record)
	assert.Equal(t, "[Image #1] [Image #2] compare", got.Text)
	assert.Equal(t, "--wait", got.Args[0], "the editor command is split into words")
	assert.Equal(t, ".md", filepath.Ext(got.Path))
	assert.Equal(t, os.FileMode(0o600), got.Mode)
	dir := filepath.Join(fe.home, "editor")
	assert.Equal(t, dir, filepath.Dir(got.Path), "the file is in uah's home")
	info, err := os.Stat(dir)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), info.Mode().Perm())
	assert.False(t, strings.HasPrefix(got.Path, workspace), "the file is not in the workspace")
	_, err = os.Stat(got.Path)
	assert.ErrorIs(t, err, os.ErrNotExist, "the file is removed afterwards")

	dr.key(tea.KeyEnter, 0)
	dr.waitFor("• an image")
	reqs := llm.Requests()
	require.Len(t, reqs, 1)
	assert.Equal(t, []string{want}, reqs[0].UserTexts)
	assert.Len(t, reqs[0].ToolImages, 1)
}

// TestTUI_EditorRoundTripsAMultiLinePaste: a pasted multi-line draft saved
// unchanged comes back unchanged.
func TestTUI_EditorRoundTripsAMultiLinePaste(t *testing.T) {
	d := depsIn(t, "simple.jsonl", approval.ModeYolo)
	fe := useFakeEditor(t, &d, "keep")
	dr := start(t, d)
	dr.until("the session is open", func() bool { return dr.m.(bubble.Model).Exit().SessionID != "" })
	paste := "line one\n\n  line three  \nfunc f() {\n\treturn\n}"
	dr.send(tea.PasteMsg{Content: paste})
	before := dr.draft()
	require.Contains(t, before, "line three")

	dr.key('g', tea.ModCtrl)
	dr.edit(fe)
	assert.Equal(t, before, readSeen(t, fe.record).Text)
	assert.Equal(t, before, dr.draft())
}

// TestTUI_EditorFailsOrEmpties: an editor that exits non-zero keeps the
// draft and says so; saving an empty file empties the composer.
func TestTUI_EditorFailsOrEmpties(t *testing.T) {
	for _, action := range []string{"fail", "empty"} {
		t.Run(action, func(t *testing.T) {
			d := depsIn(t, "simple.jsonl", approval.ModeYolo)
			fe := useFakeEditor(t, &d, action)
			dr := start(t, d)
			dr.until("the session is open", func() bool { return dr.m.(bubble.Model).Exit().SessionID != "" })
			dr.typeText("a draft")
			dr.key('g', tea.ModCtrl)
			dr.edit(fe)
			if action == "fail" {
				assert.Contains(t, dr.view(), "the draft is unchanged")
				assert.Contains(t, dr.view(), fmt.Sprintf("editor: %s: exit status 3", filepath.Base(os.Args[0])))
				assert.Equal(t, "a draft", dr.draft())

				return
			}
			assert.Empty(t, dr.draft())
		})
	}
}

// TestTUI_EditorRefusesAnExposedDraftDir: in workspace mode, with uah's home
// in the temporary directory, which sandboxed commands write, ctrl+g opens
// no editor, says why, and keeps the draft.
func TestTUI_EditorRefusesAnExposedDraftDir(t *testing.T) {
	d := deps(t, "simple.jsonl")
	fe := useFakeEditor(t, &d, "edit")
	dr := start(t, d)
	dr.until("the session is open", func() bool { return dr.m.(bubble.Model).Exit().SessionID != "" })
	dr.typeText("a draft")
	dr.key('g', tea.ModCtrl)
	dr.waitFor("the draft is unchanged")
	assert.Contains(t, dr.view(), "sandboxed commands can write")
	assert.Nil(t, fe.run, "no editor ran")
	assert.Equal(t, "a draft", dr.draft())
	assert.NoDirExists(t, filepath.Join(fe.home, "editor"))
}
