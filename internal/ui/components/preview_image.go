package components

import (
	"bufio"
	"fmt"
	"image"
	"image/color"
	_ "image/gif" // Decoders register themselves with image.Decode
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/charmbracelet/lipgloss"
	"github.com/icichainz/sushi/internal/ui"
	"github.com/muesli/termenv"
	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/webp"
)

// ImageColors is how many colors images are drawn with
type ImageColors int

const (
	ImagesOff       ImageColors = iota // Show the image's details instead
	Images256                          // The 256-color palette
	ImagesTrueColor                    // 24-bit color
)

// DetectImageColors works out how this terminal can draw images. Half
// blocks need at least 256 colors to look like anything, and --ascii asks
// for plain characters only.
func DetectImageColors() ImageColors {
	if ui.GetIconMode() == ui.IconModeASCII {
		return ImagesOff
	}
	switch lipgloss.ColorProfile() {
	case termenv.TrueColor:
		return ImagesTrueColor
	case termenv.ANSI256:
		return Images256
	}
	return ImagesOff
}

// imageExts are the extensions of the images drawn in the preview. The
// format itself is found from the file's content.
var imageExts = map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true, ".bmp": true}

// imageKinds names the formats image.Decode reports
var imageKinds = map[string]string{
	"png":  "PNG Image",
	"jpeg": "JPEG Image",
	"gif":  "GIF Image",
	"webp": "WebP Image",
	"bmp":  "BMP Image",
}

const (
	// thumbSide is the longest side of the copy of an image kept for
	// drawing: more than any pane shows, far less than a photo
	thumbSide = 400
	// imageCacheSize is how many images are kept ready to draw, so moving
	// back and forth between photos doesn't decode them again
	imageCacheSize = 8
)

// maxImagePixels is the largest image decoded, as a decoded photo takes
// several bytes a pixel. A variable so tests can lower it.
var maxImagePixels = 50_000_000

// decodeSlots limits how many images are decoded at once: moving quickly
// through a folder of photos starts a load for each
var decodeSlots = make(chan struct{}, 2)

// ImagePreview is an image ready to draw at any size
type ImagePreview struct {
	Format        string // "png", "jpeg", ...
	Width, Height int    // Of the original image
	thumb         *image.NRGBA
	colors        ImageColors

	mu    sync.Mutex
	rows  []string // Last drawn, reused while the pane keeps its size
	rowsW int
	rowsH int
}

// imageKey identifies a version of a file in the image cache
type imageKey struct {
	path   string
	mod    int64
	size   int64
	colors ImageColors
}

var imageCache = struct {
	sync.Mutex
	order []imageKey // Oldest first
	items map[imageKey]*ImagePreview
}{items: make(map[imageKey]*ImagePreview)}

// cachedImage returns the image stored under key, or decodes and stores it
func cachedImage(key imageKey) (*ImagePreview, error) {
	imageCache.Lock()
	img, ok := imageCache.items[key]
	imageCache.Unlock()
	if ok {
		return img, nil
	}

	img, err := decodeImage(key.path, key.colors)
	if err != nil {
		return nil, err
	}

	imageCache.Lock()
	defer imageCache.Unlock()
	if _, ok := imageCache.items[key]; !ok {
		imageCache.order = append(imageCache.order, key)
		imageCache.items[key] = img
	}
	for len(imageCache.order) > imageCacheSize {
		delete(imageCache.items, imageCache.order[0])
		imageCache.order = imageCache.order[1:]
	}
	return img, nil
}

// loadImagePreview draws an image in the pane, or shows its details when
// the terminal can't draw it or it is too large. The name, the file's or
// its link target's, gives the kind until the image says what it is.
func loadImagePreview(p PreviewContent, name string, config PreviewConfig) PreviewContent {
	p.Kind = getFileType(strings.ToLower(filepath.Ext(name)))
	info, err := os.Stat(p.Path)
	if err != nil {
		p.Error = err
		return withLines(p, fmt.Sprintf("Error reading file: %v", err))
	}
	cfg, format, err := decodeConfig(p.Path)
	if err != nil {
		return detailsView(p, "Cannot preview image: "+err.Error())
	}
	p.Kind = imageKinds[format]
	p.Details = []string{fmt.Sprintf("%dx%d", cfg.Width, cfg.Height)}

	switch pixels := int64(cfg.Width) * int64(cfg.Height); {
	case config.Images == ImagesOff:
		return detailsView(p, "Images are drawn in terminals with 256 colors or more, and not with --ascii")
	case pixels > int64(maxImagePixels):
		return detailsView(p, fmt.Sprintf("Too large to draw (%.0f megapixels)", float64(pixels)/1e6))
	case config.Quick:
		p.Pending = true
		return detailsView(p, "Loading...")
	}

	img, err := cachedImage(imageKey{p.Path, info.ModTime().UnixNano(), info.Size(), config.Images})
	if err != nil {
		return detailsView(p, "Cannot preview image: "+err.Error())
	}
	p.Image = img
	return p
}

// decodeConfig reads an image's size and format from its header
func decodeConfig(path string) (cfg image.Config, format string, err error) {
	defer recoverAs(&err)
	f, err := os.Open(path)
	if err != nil {
		return cfg, "", err
	}
	defer f.Close()
	return image.DecodeConfig(bufio.NewReader(f))
}

// decodeImage decodes an image and keeps a small copy of it; the full
// image is dropped as soon as the copy is made
func decodeImage(path string, colors ImageColors) (img *ImagePreview, err error) {
	decodeSlots <- struct{}{}
	defer func() { <-decodeSlots }()
	defer recoverAs(&err)

	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	full, format, err := image.Decode(bufio.NewReader(f))
	if err != nil {
		return nil, err
	}
	b := full.Bounds()
	if b.Empty() {
		return nil, fmt.Errorf("image has no pixels")
	}
	w, h := fitSize(b.Dx(), b.Dy(), thumbSide, thumbSide, false)
	return &ImagePreview{
		Format: format,
		Width:  b.Dx(),
		Height: b.Dy(),
		thumb:  resample(full, w, h),
		colors: colors,
	}, nil
}

// fitSize scales w x h to fit within maxW x maxH, keeping its shape and
// never going below one pixel. Smaller images are enlarged only if grow.
func fitSize(w, h, maxW, maxH int, grow bool) (int, int) {
	if w <= 0 || h <= 0 || maxW <= 0 || maxH <= 0 {
		return 0, 0
	}
	scale := min(float64(maxW)/float64(w), float64(maxH)/float64(h))
	if !grow {
		scale = min(scale, 1)
	}
	fw := min(max(int(float64(w)*scale+0.5), 1), maxW)
	fh := min(max(int(float64(h)*scale+0.5), 1), maxH)
	return fw, fh
}

// resample scales src to w x h pixels. Each new pixel averages a grid of
// up to 4x4 samples over the area it covers: close to a true average for
// downscaling, at a fixed cost however large the source is.
func resample(src image.Image, w, h int) *image.NRGBA {
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	b := src.Bounds()
	sx := float64(b.Dx()) / float64(w)
	sy := float64(b.Dy()) / float64(h)
	nx := min(max(int(sx+0.999), 1), 4)
	ny := min(max(int(sy+0.999), 1), 4)
	fast, _ := src.(image.RGBA64Image) // Avoids a color allocation per sample

	at := func(x, y int) color.RGBA64 {
		if fast != nil {
			return fast.RGBA64At(x, y)
		}
		r, g, bl, a := src.At(x, y).RGBA()
		return color.RGBA64{uint16(r), uint16(g), uint16(bl), uint16(a)}
	}

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			// Sums of premultiplied values, so transparent pixels add no color
			var r, g, bl, a uint64
			for j := 0; j < ny; j++ {
				py := b.Min.Y + min(int((float64(y)+(float64(j)+0.5)/float64(ny))*sy), b.Dy()-1)
				for i := 0; i < nx; i++ {
					px := b.Min.X + min(int((float64(x)+(float64(i)+0.5)/float64(nx))*sx), b.Dx()-1)
					c := at(px, py)
					r += uint64(c.R)
					g += uint64(c.G)
					bl += uint64(c.B)
					a += uint64(c.A)
				}
			}
			if a == 0 {
				continue // Stays transparent
			}
			// Back to 8-bit straight alpha: color/alpha, then the average alpha
			n := uint64(nx * ny)
			dst.SetNRGBA(x, y, color.NRGBA{
				R: uint8(r * 0xff / a),
				G: uint8(g * 0xff / a),
				B: uint8(bl * 0xff / a),
				A: uint8(a / n >> 8),
			})
		}
	}
	return dst
}

// Rows draws the image to fit width x height cells, centered across and
// from the top. Each cell is an upper half block colored with two pixels,
// which are about square in a terminal cell. There are always height rows
// of exactly width cells.
func (ip *ImagePreview) Rows(width, height int) []string {
	if width <= 0 || height <= 0 {
		return nil
	}
	ip.mu.Lock()
	defer ip.mu.Unlock()
	if ip.rows == nil || ip.rowsW != width || ip.rowsH != height {
		ip.rows, ip.rowsW, ip.rowsH = ip.draw(width, height), width, height
	}
	return ip.rows
}

// draw renders the image at a new pane size
func (ip *ImagePreview) draw(width, height int) []string {
	w, h := fitSize(ip.Width, ip.Height, width, height*2, true)
	img := resample(ip.thumb, w, h)
	left := (width - w) / 2

	rows := make([]string, height)
	var buf []byte
	for row := range rows {
		buf = append(buf[:0], spaces(left)...)
		if row*2 < h {
			buf = ip.appendRow(buf, img, row*2)
		} else {
			buf = append(buf, spaces(w)...)
		}
		buf = append(buf, spaces(width-left-w)...)
		rows[row] = string(buf)
	}
	return rows
}

// noColor is the terminal's own color, for pixels that are transparent
const noColor = -1

// appendRow draws image rows y and y+1 as one row of half blocks,
// changing colors only where they change
func (ip *ImagePreview) appendRow(buf []byte, img *image.NRGBA, y int) []byte {
	fg, bg := noColor, noColor // What the terminal is set to
	for x := 0; x < img.Rect.Dx(); x++ {
		top := ip.colorCode(img, x, y)
		bottom := ip.colorCode(img, x, y+1)

		// The upper block shows the top pixel over the bottom one; the
		// lower block is for a transparent top over an opaque bottom
		glyph, wantFG, wantBG := "▀", top, bottom
		switch {
		case top == noColor && bottom == noColor:
			glyph, wantFG = " ", fg
		case top == noColor:
			glyph, wantFG, wantBG = "▄", bottom, noColor
		}
		if wantFG != fg {
			buf = ip.appendColor(buf, 38, wantFG)
			fg = wantFG
		}
		if wantBG != bg {
			buf = ip.appendColor(buf, 48, wantBG)
			bg = wantBG
		}
		buf = append(buf, glyph...)
	}
	if fg != noColor || bg != noColor {
		buf = append(buf, "\x1b[0m"...)
	}
	return buf
}

// colorCode returns the pixel's color as the terminal is told it: a
// palette index or packed RGB, or noColor for a transparent pixel
func (ip *ImagePreview) colorCode(img *image.NRGBA, x, y int) int {
	if y >= img.Rect.Dy() {
		return noColor
	}
	c := img.NRGBAAt(x, y)
	if c.A < 0x80 {
		return noColor
	}
	if ip.colors == Images256 {
		return xterm256(c)
	}
	return int(c.R)<<16 | int(c.G)<<8 | int(c.B)
}

// appendColor sets the foreground (38) or background (48) color
func (ip *ImagePreview) appendColor(buf []byte, layer, code int) []byte {
	buf = append(buf, "\x1b["...)
	if code == noColor {
		// 39 and 49 go back to the terminal's own colors
		buf = strconv.AppendInt(buf, int64(layer+1), 10)
		return append(buf, 'm')
	}
	buf = strconv.AppendInt(buf, int64(layer), 10)
	if ip.colors == Images256 {
		buf = append(buf, ";5;"...)
		buf = strconv.AppendInt(buf, int64(code), 10)
	} else {
		buf = append(buf, ";2;"...)
		buf = strconv.AppendInt(buf, int64(code>>16), 10)
		buf = append(buf, ';')
		buf = strconv.AppendInt(buf, int64(code>>8&0xff), 10)
		buf = append(buf, ';')
		buf = strconv.AppendInt(buf, int64(code&0xff), 10)
	}
	return append(buf, 'm')
}

// xterm256 returns the nearest color of the 256-color palette, from its
// 6x6x6 cube or its gray ramp
func xterm256(c color.NRGBA) int {
	levels := [6]int{0, 95, 135, 175, 215, 255}
	step := func(v uint8) int {
		switch {
		case v < 48:
			return 0
		case v < 115:
			return 1
		}
		return (int(v) - 35) / 40
	}
	dist := func(r, g, b int) int {
		dr, dg, db := r-int(c.R), g-int(c.G), b-int(c.B)
		return dr*dr + dg*dg + db*db
	}

	r, g, b := step(c.R), step(c.G), step(c.B)
	gray := min(max((int(c.R)+int(c.G)+int(c.B))/3-3, 0)/10, 23)
	level := 8 + 10*gray
	if dist(level, level, level) < dist(levels[r], levels[g], levels[b]) {
		return 232 + gray
	}
	return 16 + 36*r + 6*g + b
}
