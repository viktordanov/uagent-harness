// Package images carries images pasted into the prompt. A message keeps its
// text with the placeholders the user saw ("[Image #1]") and gets one tag
// line per image at its end, naming the image's file in the store. The tags
// travel with the text through the session, the engine, and the runner's
// inbox, and the embedded engine turns them into image input for the model
// (see docs/design/images.md). Display strips them again.
package images

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Image is one attached image: the placeholder that stands for it in the
// text and the file that holds it in the store.
type Image struct {
	// Label is the placeholder, "[Image #1]".
	Label string
	// Ref is the file's name in the store: a SHA-256 and an extension.
	Ref string
	// Width and Height are the pixels sent, after any downscaling.
	Width, Height int
}

// Label is the placeholder for the nth image of a message, as Codex and
// Claude Code write it.
func Label(n int) string { return "[Image #" + strconv.Itoa(n) + "]" }

// tagPrefix starts every tag line; nothing a user types is likely to.
const tagPrefix = "<uah-image "

// tagRE matches one tag line.
var tagRE = regexp.MustCompile(`^<uah-image label="(\[Image #\d+\])" ref="([0-9a-f]{64}\.(?:png|jpg))" size="(\d+)x(\d+)"/>$`)

// Tag is the line that attaches an image to a message.
func Tag(img Image) string {
	return fmt.Sprintf(`%slabel=%q ref=%q size="%dx%d"/>`, tagPrefix, img.Label, img.Ref, img.Width, img.Height)
}

// Join appends a tag line per image to the text. With no images the text
// is unchanged.
func Join(text string, imgs []Image) string {
	if len(imgs) == 0 {
		return text
	}
	var b strings.Builder
	b.WriteString(strings.TrimRight(text, "\n"))
	b.WriteString("\n")
	for _, img := range imgs {
		b.WriteString("\n")
		b.WriteString(Tag(img))
	}

	return b.String()
}

// Split takes the tag lines off the end of a message: the text as the user
// wrote it, and its images in order. Text without tags comes back as it is.
func Split(text string) (string, []Image) {
	if !strings.Contains(text, tagPrefix) {
		return text, nil
	}
	lines := strings.Split(text, "\n")
	end := len(lines)
	var imgs []Image
	for end > 0 {
		img, ok := parseTag(lines[end-1])
		if !ok {
			break
		}
		imgs = append([]Image{img}, imgs...)
		end--
	}
	if len(imgs) == 0 {
		return text, nil
	}

	return strings.TrimRight(strings.Join(lines[:end], "\n"), "\n"), imgs
}

// Display is the text as the transcript shows it: the placeholders stay,
// the tags go.
func Display(text string) string {
	out, _ := Split(text)

	return out
}

func parseTag(line string) (Image, bool) {
	m := tagRE.FindStringSubmatch(strings.TrimSpace(line))
	if m == nil {
		return Image{}, false
	}
	w, errW := strconv.Atoi(m[3])
	h, errH := strconv.Atoi(m[4])
	if errW != nil || errH != nil {
		return Image{}, false
	}

	return Image{Label: m[1], Ref: m[2], Width: w, Height: h}, true
}
