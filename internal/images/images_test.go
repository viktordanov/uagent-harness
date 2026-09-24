package images_test

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent-harness/internal/images"
)

func pngOf(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for x := range w {
		img.Set(x, 0, color.NRGBA{R: 255, A: 255})
	}
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))

	return buf.Bytes()
}

func TestJoinSplit_RoundTrip(t *testing.T) {
	ref := strings.Repeat("a", 64) + ".png"
	imgs := []images.Image{
		{Label: images.Label(1), Ref: ref, Width: 10, Height: 20},
		{Label: images.Label(2), Ref: strings.Repeat("b", 64) + ".jpg", Width: 3, Height: 4},
	}
	text := images.Join("look at [Image #1] and [Image #2]", imgs)
	assert.Contains(t, text, `<uah-image label="[Image #1]" ref="`+ref+`" size="10x20"/>`)

	got, parsed := images.Split(text)
	assert.Equal(t, "look at [Image #1] and [Image #2]", got)
	assert.Equal(t, imgs, parsed)
	assert.Equal(t, got, images.Display(text))
}

func TestSplit_LeavesOtherText(t *testing.T) {
	for _, text := range []string{"plain", `<uah-image label="x"/> is not a tag`, "a\n<uah-image nope/>"} {
		got, imgs := images.Split(text)
		assert.Equal(t, text, got)
		assert.Empty(t, imgs)
	}
	assert.Equal(t, "hi", images.Join("hi", nil))
}

func TestStore_PutAndDataURL(t *testing.T) {
	s := images.Store{Dir: t.TempDir()}
	img, err := s.Put(pngOf(t, 40, 30))
	require.NoError(t, err)
	assert.Equal(t, 40, img.Width)
	assert.True(t, strings.HasSuffix(img.Ref, ".png"))

	again, err := s.Put(pngOf(t, 40, 30))
	require.NoError(t, err)
	assert.Equal(t, img.Ref, again.Ref, "the store is content-addressed")

	url, err := s.DataURL(img.Ref)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(url, "data:image/png;base64,"))
	data, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(url, "data:image/png;base64,"))
	require.NoError(t, err)
	cfg, err := png.DecodeConfig(bytes.NewReader(data))
	require.NoError(t, err)
	assert.Equal(t, 40, cfg.Width)

	_, err = s.DataURL("../etc/passwd")
	require.Error(t, err)
}

func TestPrepare_Downscales(t *testing.T) {
	data, ext, w, h, err := images.Prepare(pngOf(t, 4000, 1000))
	require.NoError(t, err)
	assert.Equal(t, ".png", ext)
	assert.Equal(t, [2]int{images.MaxSide, 500}, [2]int{w, h})
	cfg, err := png.DecodeConfig(bytes.NewReader(data))
	require.NoError(t, err)
	assert.Equal(t, images.MaxSide, cfg.Width)
}

func TestPrepare_JPEGPassesThrough(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, jpeg.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 8, 8)), nil))
	data, ext, _, _, err := images.Prepare(buf.Bytes())
	require.NoError(t, err)
	assert.Equal(t, ".jpg", ext)
	assert.Equal(t, buf.Bytes(), data)
}

func TestPrepare_RefusesNonImages(t *testing.T) {
	_, _, _, _, err := images.Prepare([]byte("hello"))
	require.Error(t, err)
}

func TestPastedPath(t *testing.T) {
	dir := t.TempDir()
	plain := filepath.Join(dir, "shot.png")
	spaced := filepath.Join(dir, "my shot.PNG")
	text := filepath.Join(dir, "notes.txt")
	for _, p := range []string{plain, spaced, text} {
		require.NoError(t, os.WriteFile(p, []byte("x"), 0o600))
	}
	cases := []struct {
		in   string
		want string
	}{
		{plain, plain},
		{"  " + plain + "\n", plain},
		{"'" + spaced + "'", spaced},
		{`"` + spaced + `"`, spaced},
		{strings.ReplaceAll(spaced, " ", `\ `), spaced},
		{"file://" + plain, plain},
		{"file://" + strings.ReplaceAll(spaced, " ", "%20"), spaced},
		{"~/shot.png", plain},
		{spaced, ""}, // two words
		{text, ""},   // not an image
		{filepath.Join(dir, "missing.png"), ""},
		{"shot.png", ""}, // relative
		{"see " + plain, ""},
		{plain + "\n" + plain, ""}, // two lines
	}
	for _, c := range cases {
		got, ok := images.PastedPath(c.in, dir)
		assert.Equal(t, c.want != "", ok, c.in)
		assert.Equal(t, c.want, got, c.in)
	}
}
