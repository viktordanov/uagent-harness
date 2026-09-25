package embedded_test

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent-harness/internal/home/migrate"
	"github.com/viktordanov/uagent-harness/internal/images"
	"github.com/viktordanov/uagent-harness/internal/session"
	"github.com/viktordanov/uagent-harness/testing/fakellm"
)

// TestEmbedded_SendsPastedImages: a message with a pasted image reaches the
// model as its text without the tag, followed by the image as a ViewImage
// result, and later requests of the session carry it again.
func TestEmbedded_SendsPastedImages(t *testing.T) {
	e := newEnv(t, fakellm.Reply{Text: "a square"}, fakellm.Reply{Text: "still a square"})
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, image.NewNRGBA(image.Rect(0, 0, 12, 8))))
	store := images.Store{Dir: images.DirIn(e.StateDir)}
	img, err := store.Put(buf.Bytes())
	require.NoError(t, err)
	img.Label = images.Label(1)
	gone := images.Image{Label: images.Label(2), Ref: strings.Repeat("0", 64) + ".png", Width: 1, Height: 1}
	url, err := store.DataURL(img.Ref)
	require.NoError(t, err)

	s, ev := e.open(t, e.embedded(), "")
	_, err = s.Submit(images.Join("what is [Image #1] next to [Image #2]?", []images.Image{img, gone}))
	require.NoError(t, err)
	ev.finished()
	ev.idle()
	_, err = s.Submit("and now?")
	require.NoError(t, err)
	ev.finished()
	ev.idle()

	reqs := e.llm.Requests()
	require.Len(t, reqs, 2)
	first := reqs[0]
	assert.Equal(t, []string{"what is [Image #1] next to [Image #2]?"}, first.UserTexts, "the tags never reach the model")
	assert.Equal(t, []string{url}, first.ToolImages)
	assert.Equal(t, []string{"uah_image_1_1", "uah_image_1_2"}, first.CallIDs)
	require.Len(t, first.ToolOutputs, 2)
	assert.Contains(t, first.ToolOutputs[0], "[Image #1], pasted by the user")
	assert.Contains(t, first.ToolOutputs[1], "[Image #2] pasted by the user is no longer available")

	assert.Equal(t, []string{url}, reqs[1].ToolImages, "the image stays in the conversation")
	assert.Equal(t, "and now?", reqs[1].UserTexts[len(reqs[1].UserTexts)-1])
}

// TestEmbedded_ResumesAMigratedSession: a session with a pasted image, copied
// into a new home by the migration, resumes there after the old state
// directory is gone. The image travels by reference, so the resumed session
// still sends it, and the TUI's history loads from the new home.
func TestEmbedded_ResumesAMigratedSession(t *testing.T) {
	e := newEnv(t, fakellm.Reply{Text: "a square"}, fakellm.Reply{Text: "still a square"})
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, image.NewNRGBA(image.Rect(0, 0, 12, 8))))
	img, err := images.Store{Dir: images.DirIn(e.StateDir)}.Put(buf.Bytes())
	require.NoError(t, err)
	img.Label = images.Label(1)
	url, err := images.Store{Dir: images.DirIn(e.StateDir)}.DataURL(img.Ref)
	require.NoError(t, err)
	s, ev := e.open(t, e.embedded(), "")
	_, err = s.Submit(images.Join("what is [Image #1]?", []images.Image{img}))
	require.NoError(t, err)
	ev.finished()
	ev.idle()
	id := s.ID()
	require.NoError(t, s.Close())

	newHome := filepath.Join(t.TempDir(), ".uah")
	migrated, err := migrate.Run(context.Background(), migrate.Paths{Home: newHome, State: e.StateDir})
	require.NoError(t, err)
	require.True(t, migrated)
	require.NoError(t, os.RemoveAll(e.StateDir))
	e.StateDir = newHome

	history, err := session.Load(newHome, id)
	require.NoError(t, err)
	require.Len(t, history, 1)
	resumed, ev := e.open(t, e.embedded(), id)
	_, err = resumed.Submit("and now?")
	require.NoError(t, err)
	ev.finished()
	ev.idle()

	reqs := e.llm.Requests()
	require.Len(t, reqs, 2)
	assert.Equal(t, []string{url}, reqs[1].ToolImages, "the image resolves in the new home")
	assert.Equal(t, "what is [Image #1]?", reqs[1].UserTexts[0])
}
