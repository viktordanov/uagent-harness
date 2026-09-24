package images

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"regexp"

	// The formats a pasted image may come in, as the runner's ViewImage
	// reads them.
	_ "image/gif"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"

	"golang.org/x/image/draw"
)

// The limits match the runner's ViewImage tool (harness/tool/viewimage), so a
// pasted image and an image the model opens itself are sized alike.
const (
	// MaxSide is the longest side sent; larger images are downscaled.
	MaxSide = 2000
	// MaxEncoded is the most base64 bytes one image may take in a request.
	MaxEncoded = 5_000_000 - 1_000
	// MaxPixels bounds the decoding work for one image.
	MaxPixels = 32_000_000
	// MaxSource is the largest file or clipboard content read.
	MaxSource = 64 << 20
	// jpegQuality is the quality of a re-encoded JPEG, as Codex's.
	jpegQuality = 85
)

// ErrTooLarge means the image cannot fit the provider's limit.
var ErrTooLarge = errors.New("the image is too large to send")

// Store keeps pasted images as files named by their content, so a message
// names its images by reference and a resumed session finds them again.
type Store struct{ Dir string }

// DirIn is the image store of a state directory.
func DirIn(stateDir string) string { return filepath.Join(stateDir, "images") }

// Put prepares an image for the model (downscaled to MaxSide, re-encoded
// when needed) and saves it. The Image it returns has no label yet.
func (s Store) Put(data []byte) (Image, error) {
	encoded, ext, w, h, err := Prepare(data)
	if err != nil {
		return Image{}, err
	}
	sum := sha256.Sum256(encoded)
	ref := hex.EncodeToString(sum[:]) + ext
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return Image{}, fmt.Errorf("failed to create the image store: %w", err)
	}
	path := filepath.Join(s.Dir, ref)
	if _, err := os.Stat(path); err != nil {
		if err := writeFile(path, encoded); err != nil {
			return Image{}, err
		}
	}

	return Image{Ref: ref, Width: w, Height: h}, nil
}

// PutFile reads an image file and puts it in the store.
func (s Store) PutFile(path string) (Image, error) {
	f, err := os.Open(path) //nolint:gosec // the user chose the file
	if err != nil {
		return Image{}, fmt.Errorf("failed to open the image: %w", err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, MaxSource+1))
	if err != nil {
		return Image{}, fmt.Errorf("failed to read the image: %w", err)
	}
	if len(data) > MaxSource {
		return Image{}, fmt.Errorf("%w: the file is over %d MiB", ErrTooLarge, MaxSource>>20)
	}

	return s.Put(data)
}

// refRE is a valid store reference, so a tag cannot name a path outside it.
var refRE = regexp.MustCompile(`^[0-9a-f]{64}\.(png|jpg)$`)

// DataURL is the stored image as a data URL, as the Responses API takes it.
func (s Store) DataURL(ref string) (string, error) {
	m := refRE.FindStringSubmatch(ref)
	if m == nil {
		return "", fmt.Errorf("invalid image reference %q", ref)
	}
	data, err := os.ReadFile(filepath.Join(s.Dir, ref))
	if err != nil {
		return "", fmt.Errorf("failed to read the pasted image: %w", err)
	}
	mime := "image/png"
	if m[1] == "jpg" {
		mime = "image/jpeg"
	}

	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data), nil
}

// Prepare decodes an image, downscales it to fit MaxSide, and encodes it:
// a JPEG that needs no scaling passes through, anything else becomes PNG,
// and a PNG over MaxEncoded becomes JPEG. It returns the bytes, their file
// extension, and the size sent.
func Prepare(data []byte) (encoded []byte, ext string, width, height int, err error) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, "", 0, 0, fmt.Errorf("not an image uah can read (PNG, JPEG, GIF, WebP, BMP, TIFF): %w", err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > MaxPixels/cfg.Height {
		return nil, "", 0, 0, fmt.Errorf("%w: %dx%d pixels", ErrTooLarge, cfg.Width, cfg.Height)
	}
	w, h := fit(cfg.Width, cfg.Height)
	if format == "jpeg" && w == cfg.Width && h == cfg.Height && base64.StdEncoding.EncodedLen(len(data)) <= MaxEncoded {
		return data, ".jpg", w, h, nil
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, "", 0, 0, fmt.Errorf("failed to decode the %s image: %w", format, err)
	}
	if w != cfg.Width || h != cfg.Height {
		dst := image.NewNRGBA(image.Rect(0, 0, w, h))
		draw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Src, nil)
		src = dst
	}
	encoded, ext, err = encode(src, format == "jpeg")
	if err != nil {
		return nil, "", 0, 0, err
	}

	return encoded, ext, w, h, nil
}

// encode writes PNG, or JPEG for a JPEG source or a PNG that is too large.
func encode(img image.Image, jpegFirst bool) ([]byte, string, error) {
	var buf bytes.Buffer
	if !jpegFirst {
		if err := png.Encode(&buf, img); err != nil {
			return nil, "", fmt.Errorf("failed to encode the image: %w", err)
		}
		if base64.StdEncoding.EncodedLen(buf.Len()) <= MaxEncoded {
			return buf.Bytes(), ".png", nil
		}
		buf.Reset()
	}
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: jpegQuality}); err != nil {
		return nil, "", fmt.Errorf("failed to encode the image: %w", err)
	}
	if base64.StdEncoding.EncodedLen(buf.Len()) > MaxEncoded {
		return nil, "", fmt.Errorf("%w: %d bytes after downscaling, the limit is %d", ErrTooLarge, buf.Len(), MaxEncoded*3/4)
	}

	return buf.Bytes(), ".jpg", nil
}

// fit scales a size down to fit MaxSide on both sides, keeping its ratio.
func fit(w, h int) (int, int) {
	if w <= MaxSide && h <= MaxSide {
		return w, h
	}
	if w >= h {
		return MaxSide, max(1, h*MaxSide/w)
	}

	return max(1, w*MaxSide/h), MaxSide
}

// writeFile writes through a temporary file and a rename, so a reader never
// sees half an image.
func writeFile(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".image-*")
	if err != nil {
		return fmt.Errorf("failed to save the image: %w", err)
	}
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if err := errors.Join(werr, cerr); err != nil {
		_ = os.Remove(tmp.Name())

		return fmt.Errorf("failed to save the image: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		_ = os.Remove(tmp.Name())

		return fmt.Errorf("failed to save the image: %w", err)
	}

	return nil
}
