package pdf

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/draw"
	_ "image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-pdf/fpdf"
	_ "golang.org/x/image/bmp"
	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"
)

// Limits that keep memory use predictable on huge screenshots and scans.
const (
	maxImageFileBytes = 40 << 20   // larger files are skipped
	maxImagePixels    = 60_000_000 // larger images are skipped (would need ~240 MB to decode)
	downscalePixels   = 5_000_000  // larger images are shrunk before embedding
	maxImageSide      = 2400       // longest side after shrinking, in pixels
)

type loadedImage struct {
	name          string
	kind          string // fpdf image type: JPG, PNG or GIF
	width, height int    // pixels
}

var errSkipImage = errors.New("image skipped")

// loadImage registers a local image with the document, converting or
// shrinking it when needed. dest is the link target from the Markdown.
func (r *renderer) loadImage(dest string) (loadedImage, error) {
	if i := strings.IndexAny(dest, "?#"); i >= 0 {
		dest = dest[:i]
	}
	lower := strings.ToLower(dest)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "data:") {
		return loadedImage{}, fmt.Errorf("%w: online images are not downloaded", errSkipImage)
	}
	rel, err := url.PathUnescape(dest)
	if err != nil {
		rel = dest
	}
	p := filepath.Join(r.dir, filepath.FromSlash(rel))
	if img, ok := r.images[p]; ok {
		return img, nil
	}

	f, err := os.Open(p)
	if err != nil {
		return loadedImage{}, fmt.Errorf("%w: file not found", errSkipImage)
	}
	defer f.Close()
	if fi, err := f.Stat(); err == nil && fi.Size() > maxImageFileBytes {
		return loadedImage{}, fmt.Errorf("%w: file is too large (%d MB)", errSkipImage, fi.Size()>>20)
	}
	cfg, format, err := image.DecodeConfig(f)
	if err != nil {
		return loadedImage{}, fmt.Errorf("%w: unsupported image format", errSkipImage)
	}
	if cfg.Width*cfg.Height > maxImagePixels {
		return loadedImage{}, fmt.Errorf("%w: image is too large (%dx%d)", errSkipImage, cfg.Width, cfg.Height)
	}

	img := loadedImage{name: p, width: cfg.Width, height: cfg.Height}
	direct := map[string]string{"jpeg": "JPG", "png": "PNG", "gif": "GIF"}[format]
	if direct != "" && cfg.Width*cfg.Height <= downscalePixels {
		if _, err := f.Seek(0, io.SeekStart); err == nil {
			r.pdf.RegisterImageOptionsReader(p, fpdf.ImageOptions{ImageType: direct}, f)
			if !r.pdf.Err() {
				img.kind = direct
				r.images[p] = img
				return img, nil
			}
			// For example interlaced or 16-bit PNG: convert below.
			r.pdf.ClearError()
		}
	}

	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return loadedImage{}, err
	}
	data, kind, w, h, err := normalizeImage(f)
	if err != nil {
		return loadedImage{}, fmt.Errorf("%w: %v", errSkipImage, err)
	}
	r.pdf.RegisterImageOptionsReader(p, fpdf.ImageOptions{ImageType: kind}, bytes.NewReader(data))
	if r.pdf.Err() {
		err := r.pdf.Error()
		r.pdf.ClearError()
		return loadedImage{}, fmt.Errorf("%w: %v", errSkipImage, err)
	}
	img.kind, img.width, img.height = kind, w, h
	r.images[p] = img
	return img, nil
}

// normalizeImage decodes any supported image, shrinks it if it is very
// large, and re-encodes it as JPEG (opaque) or 8-bit PNG (transparent).
func normalizeImage(rd io.Reader) (data []byte, kind string, w, h int, err error) {
	src, _, err := image.Decode(rd)
	if err != nil {
		return nil, "", 0, 0, err
	}
	b := src.Bounds()
	w, h = b.Dx(), b.Dy()
	if w > maxImageSide || h > maxImageSide {
		scale := float64(maxImageSide) / float64(max(w, h))
		w, h = max(int(float64(w)*scale), 1), max(int(float64(h)*scale), 1)
		dst := image.NewNRGBA(image.Rect(0, 0, w, h))
		xdraw.ApproxBiLinear.Scale(dst, dst.Bounds(), src, b, draw.Src, nil)
		src = dst
	}
	var buf bytes.Buffer
	if o, ok := src.(interface{ Opaque() bool }); ok && o.Opaque() {
		err = jpeg.Encode(&buf, src, &jpeg.Options{Quality: 85})
		return buf.Bytes(), "JPG", w, h, err
	}
	nrgba := image.NewNRGBA(image.Rect(0, 0, w, h))
	draw.Draw(nrgba, nrgba.Bounds(), src, src.Bounds().Min, draw.Src)
	err = png.Encode(&buf, nrgba)
	return buf.Bytes(), "PNG", w, h, err
}
