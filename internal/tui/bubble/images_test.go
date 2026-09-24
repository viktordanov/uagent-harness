package bubble_test

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent-harness/internal/engine/embedded"
	"github.com/viktordanov/uagent-harness/internal/images"
	"github.com/viktordanov/uagent-harness/internal/images/clipboard"
	"github.com/viktordanov/uagent-harness/internal/session"
	"github.com/viktordanov/uagent-harness/internal/tui/bubble"
	"github.com/viktordanov/uagent-harness/testing/fakellm"
	"github.com/viktordanov/uagent-harness/testing/harnesstest"
)

// fakeClipboard holds an image; no test reads the real clipboard.
type fakeClipboard struct{ content clipboard.Content }

func (f fakeClipboard) ReadImage(context.Context) (clipboard.Content, error) { return f.content, nil }

func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, image.NewNRGBA(image.Rect(0, 0, w, h))))

	return buf.Bytes()
}

// imageDeps opens sessions on the embedded engine with fakellm, a fake
// clipboard holding a PNG, and the image store in the state directory.
func imageDeps(t *testing.T) (bubble.Deps, *fakellm.Server, string) {
	t.Helper()
	env := harnesstest.NewEnv(t)
	llm := fakellm.New(t, fakellm.Reply{Text: "an image"})
	getenv := func(key string) string {
		if key == "OPENAI_API_KEY" {
			return "test-key"
		}

		return env.Getenv(key)
	}
	eng := embedded.New(embedded.Config{StateDir: env.StateDir, Provider: "openai", Getenv: getenv})
	settings := session.Settings{Provider: "openai", Model: "gpt-test", Effort: "high", Workspace: env.Workspace, BaseURL: llm.URL}

	return bubble.Deps{
		Open: func(ctx context.Context, id string) (*session.Session, []session.LoadedRun, error) {
			s, err := session.Open(ctx, eng, session.Options{ID: id, Settings: settings, Interactive: true})

			return s, nil, err
		},
		Sessions:  func() ([]session.Info, error) { return nil, nil },
		Images:    &images.Store{Dir: images.DirIn(env.StateDir)},
		Clipboard: fakeClipboard{content: clipboard.Content{Data: pngBytes(t, 30, 20)}},
	}, llm, env.Workspace
}

// TestTUI_PasteImages: ctrl+v puts a placeholder in the composer, one
// backspace removes a whole placeholder and its image, and the image left
// goes to the model with the message.
func TestTUI_PasteImages(t *testing.T) {
	d, llm, _ := imageDeps(t)
	dr := start(t, d)
	dr.until("the session is open", func() bool { return dr.m.(bubble.Model).Exit().SessionID != "" })

	dr.key('v', tea.ModCtrl)
	dr.waitFor("λ [Image #1]")
	dr.key('v', tea.ModAlt)
	dr.waitFor("[Image #1] [Image #2]")

	dr.key(tea.KeyBackspace, 0) // the space after it
	dr.key(tea.KeyBackspace, 0) // the whole placeholder
	assert.NotContains(t, dr.view(), "[Image #2")

	dr.typeText("what is this?")
	dr.key(tea.KeyEnter, 0)
	dr.waitFor("• an image")
	assert.Contains(t, dr.view(), "λ [Image #1] what is this?")
	assert.NotContains(t, dr.view(), "uah-image", "the transcript shows placeholders, not tags")

	reqs := llm.Requests()
	require.Len(t, reqs, 1)
	assert.Equal(t, []string{"[Image #1] what is this?"}, reqs[0].UserTexts)
	require.Len(t, reqs[0].ToolImages, 1)
	assert.True(t, strings.HasPrefix(reqs[0].ToolImages[0], "data:image/png;base64,"))
}

// TestTUI_PasteAnImagePath: a pasted or dropped image path attaches the
// file; any other paste stays text.
func TestTUI_PasteAnImagePath(t *testing.T) {
	d, _, workspace := imageDeps(t)
	shot := filepath.Join(workspace, "my shot.png")
	require.NoError(t, os.WriteFile(shot, pngBytes(t, 8, 8), 0o600))
	dr := start(t, d)
	dr.until("the session is open", func() bool { return dr.m.(bubble.Model).Exit().SessionID != "" })

	dr.send(tea.PasteMsg{Content: "'" + shot + "'"})
	dr.waitFor("λ [Image #1]")
	dr.send(tea.PasteMsg{Content: "plain words"})
	dr.waitFor("[Image #1] plain words")
	assert.NotContains(t, dr.view(), "my shot.png", "the path became an attachment")
}
