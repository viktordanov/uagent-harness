package embedded_test

import (
	"bytes"
	"image"
	"image/png"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent-harness/internal/images"
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
