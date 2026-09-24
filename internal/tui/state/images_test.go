package state_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/engine"
	"github.com/viktordanov/uagent-harness/internal/images"
	"github.com/viktordanov/uagent-harness/internal/session"
	"github.com/viktordanov/uagent-harness/internal/tui/state"
)

func stored(c string) images.Image {
	return images.Image{Ref: strings.Repeat(c, 64) + ".png", Width: 4, Height: 3}
}

func withImages() state.State {
	s := opened()
	s.Caps = engine.Capabilities{Images: true}

	return s
}

// TestImages_PlaceholdersAndSend: each attached image gets the next
// placeholder at the cursor, and on send the images the text still names go
// with the message as tags.
func TestImages_PlaceholdersAndSend(t *testing.T) {
	s, effects := apply(withImages(), state.PasteImage{})
	assert.Equal(t, []state.Effect{state.EffPasteImage{}}, effects)

	s, effects = apply(s, state.ImageAttached{Image: stored("a")})
	assert.Equal(t, []state.Effect{state.EffInsertText{Text: "[Image #1] "}}, effects)
	s, effects = apply(s, state.ImageAttached{Image: stored("b")})
	assert.Equal(t, []state.Effect{state.EffInsertText{Text: "[Image #2] "}}, effects)
	require.Len(t, s.Attached, 2)

	s, effects = apply(s, state.Submit{Text: "compare [Image #1] with [Image #2]"})
	require.Len(t, effects, 1)
	sent := effects[0].(state.EffSubmit).Text
	text, imgs := images.Split(sent)
	assert.Equal(t, "compare [Image #1] with [Image #2]", text)
	require.Len(t, imgs, 2)
	assert.Equal(t, "[Image #2]", imgs[1].Label)
	assert.Equal(t, stored("b").Ref, imgs[1].Ref)
	assert.Empty(t, s.Attached, "the draft's images went with the message")

	// The transcript shows the placeholders, not the tags, live and resumed.
	s, _ = apply(s,
		session.InputQueued{At: t0, Input: core.UserInput{ID: "m1", Text: sent}},
		session.InputSent{At: t0, IDs: []string{"m1"}},
	)
	it, ok := s.Item("msg:m1")
	require.True(t, ok)
	assert.Equal(t, "compare [Image #1] with [Image #2]", it.Text)
	resumed, _ := apply(opened(), core.UserMessage{ID: "m2", Text: sent})
	it, ok = resumed.Item("msg:m2")
	require.True(t, ok)
	assert.Equal(t, "compare [Image #1] with [Image #2]", it.Text)
}

// TestImages_DeletingThePlaceholderDropsTheImage: a placeholder gone from
// the draft drops its image, and numbering goes on after the ones left.
func TestImages_DeletingThePlaceholderDropsTheImage(t *testing.T) {
	s, _ := apply(withImages(), state.ImageAttached{Image: stored("a")}, state.ImageAttached{Image: stored("b")})
	s, _ = apply(s, state.DraftChanged{Draft: "look [Image #2] "})
	require.Len(t, s.Attached, 1)
	assert.Equal(t, "[Image #2]", s.Attached[0].Label)

	s, effects := apply(s, state.ImageAttached{Image: stored("c")})
	assert.Equal(t, []state.Effect{state.EffInsertText{Text: "[Image #3] "}}, effects)

	s, effects = apply(s, state.Submit{Text: "only text now"})
	assert.Equal(t, []state.Effect{state.EffSubmit{Text: "only text now"}}, effects, "no placeholder, no image")
	assert.Empty(t, s.Attached)

	s, _ = apply(s, state.ImageAttached{Image: stored("d")})
	assert.Equal(t, "[Image #1]", s.Attached[0].Label, "a new draft counts from 1")
}

// TestImages_WithdrawnMessageKeepsItsImages: ↑ takes a queued message back
// with its images, and sending it again sends them again.
func TestImages_WithdrawnMessageKeepsItsImages(t *testing.T) {
	img := stored("a")
	img.Label = images.Label(1)
	queued := images.Join("see [Image #1]", []images.Image{img})
	s, effects := apply(withImages(), state.DraftRestored{Text: queued})
	assert.Equal(t, []state.Effect{state.EffSetDraft{Text: "see [Image #1]"}}, effects)
	_, effects = apply(s, state.Steer{Text: "see [Image #1]"})
	assert.Equal(t, []state.Effect{state.EffSteer{Text: queued}}, effects)
}

// TestImages_PathsAndFailures: a pasted image path attaches the file; on an
// engine without images, or when the file fails, the text is pasted.
func TestImages_PathsAndFailures(t *testing.T) {
	_, effects := apply(withImages(), state.AttachFile{Path: "/tmp/a.png", Text: "'/tmp/a.png'"})
	assert.Equal(t, []state.Effect{state.EffAttachFile{Path: "/tmp/a.png", Text: "'/tmp/a.png'"}}, effects)

	s, effects := apply(withImages(), state.ImageFailed{Err: errors.New("not an image"), Text: "/tmp/a.png"})
	assert.Equal(t, []state.Effect{state.EffInsertText{Text: "/tmp/a.png"}}, effects)
	assert.Contains(t, s.Items[len(s.Items)-1].Text, "could not attach the image: not an image")

	s, effects = apply(opened(), state.AttachFile{Path: "/tmp/a.png", Text: "/tmp/a.png"})
	assert.Equal(t, []state.Effect{state.EffInsertText{Text: "/tmp/a.png"}}, effects)
	assert.Contains(t, s.Items[len(s.Items)-1].Text, "pasted images: not supported by the process engine")

	s, effects = apply(opened(), state.ImageAttached{Image: stored("a")})
	assert.Empty(t, effects)
	assert.Empty(t, s.Attached)
}

// TestImages_MentionAttachesAnImageFile: choosing an image after "@"
// attaches it in place of its path, as Codex does; other files stay text.
func TestImages_MentionAttachesAnImageFile(t *testing.T) {
	s, _ := apply(withImages(), state.FilesLoaded{Paths: []string{"docs/shot.png", "main.go"}})
	_, effects := apply(s, state.MenuAccept{Draft: "look at @shot"})
	assert.Equal(t, []state.Effect{
		state.EffSetDraft{Text: "look at "},
		state.EffAttachFile{Path: "/workspace/docs/shot.png", Text: "docs/shot.png "},
	}, effects)

	_, effects = apply(s, state.MenuAccept{Draft: "look at @main"})
	assert.Equal(t, []state.Effect{state.EffSetDraft{Text: "look at main.go "}}, effects)
}
