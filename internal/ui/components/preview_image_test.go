package components

import (
	"encoding/base64"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/icichainz/sushi/internal/ui"
	"github.com/icichainz/sushi/internal/utils"
	"github.com/muesli/termenv"
	"golang.org/x/image/bmp"
)

var (
	red         = color.NRGBA{255, 0, 0, 255}
	blue        = color.NRGBA{0, 0, 255, 255}
	transparent = color.NRGBA{}
)

// stripes is a pattern that looks different at every scale
func stripes(x, y int) color.Color {
	return color.NRGBA{uint8(x * 7), uint8(y * 11), uint8((x + y) * 3), 255}
}

// writeImage encodes a w x h image, colored by fill, in the format its
// extension names
func writeImage(t *testing.T, path string, w, h int, fill func(x, y int) color.Color) {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, fill(x, y))
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	switch filepath.Ext(path) {
	case ".png":
		err = png.Encode(f, img)
	case ".jpg":
		err = jpeg.Encode(f, img, nil)
	case ".gif":
		err = gif.Encode(f, img, nil)
	case ".bmp":
		err = bmp.Encode(f, img)
	}
	if err != nil {
		t.Fatal(err)
	}
}

// drawConfig is the default config with images drawn in the given colors
func drawConfig(colors ImageColors) PreviewConfig {
	cfg := DefaultPreviewConfig()
	cfg.Images = colors
	return cfg
}

func TestImageIsDrawnWithHalfBlocks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "flag.png")
	// Red over blue: each cell is two pixels, so 20 pixels make 10 rows
	writeImage(t, path, 40, 20, func(x, y int) color.Color {
		if y < 10 {
			return red
		}
		return blue
	})

	p := LoadPreviewWithConfig(fileInfo(t, path), drawConfig(ImagesTrueColor))
	if p.Image == nil || p.Kind != "PNG Image" || p.MaxScroll(12) != 0 {
		t.Fatalf("Image=%v Kind=%q MaxScroll=%d: %v", p.Image != nil, p.Kind, p.MaxScroll(12), p.Lines)
	}
	lines := render(t, p, 42, 12, 0)
	for _, want := range []string{"flag.png", "PNG Image", "40x20"} {
		if !strings.Contains(lines[0], want) {
			t.Errorf("heading %q is missing %q", lines[0], want)
		}
	}
	for i := 1; i <= 10; i++ {
		if got := strings.Count(lines[i], "▀"); got != 40 {
			t.Fatalf("row %d has %d half blocks, want 40:\n%s", i, got, strings.Join(lines, "\n"))
		}
	}
	if strings.TrimSpace(lines[11]) != "" {
		t.Fatalf("row below the image = %q, want blank", lines[11])
	}

	raw := RenderPreview(p, 42, 12, 0, plainStyles())
	if !strings.Contains(raw[1], "\x1b[38;2;255;0;0m\x1b[48;2;255;0;0m▀▀") {
		t.Fatalf("top row isn't red, or repeats its colors: %q", raw[1])
	}
	if !strings.Contains(raw[10], "38;2;0;0;255") || !strings.HasSuffix(raw[10], "\x1b[0m ") {
		t.Fatalf("bottom row isn't blue, or leaves its colors on: %q", raw[10])
	}

	// The 256-color palette is used when that's all the terminal has
	p256 := LoadPreviewWithConfig(fileInfo(t, path), drawConfig(Images256))
	if raw := RenderPreview(p256, 42, 12, 0, plainStyles()); !strings.Contains(raw[1], "\x1b[38;5;196m") {
		t.Fatalf("256-color row = %q, want palette red (196)", raw[1])
	}
}

func TestImageKeepsItsShapeAndIsCentered(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tall.png")
	writeImage(t, path, 20, 80, stripes)

	// 40 cells by 10 rows is 40 x 20 pixels; a 1:4 image fits as 5 x 20
	p := LoadPreviewWithConfig(fileInfo(t, path), drawConfig(ImagesTrueColor))
	lines := render(t, p, 42, 11, 0)
	for i := 1; i <= 10; i++ {
		if got := strings.Count(lines[i], "▀"); got != 5 {
			t.Fatalf("row %d has %d cells of image, want 5", i, got)
		}
		// One space of margin, then (40-5)/2 of centering
		if at := strings.Index(lines[i], "▀"); at != 1+17 {
			t.Fatalf("row %d starts at column %d, want 18", i, at)
		}
	}
}

func TestTransparentPixelsShowTheTerminal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "corners.png")
	// Left column: clear over red. Right column: red over clear.
	writeImage(t, path, 2, 2, func(x, y int) color.Color {
		if (x == 0) == (y == 1) {
			return red
		}
		return transparent
	})
	p := LoadPreviewWithConfig(fileInfo(t, path), drawConfig(ImagesTrueColor))
	row := p.Image.Rows(2, 1)[0]
	if row != "\x1b[38;2;255;0;0m▄▀\x1b[0m" {
		t.Fatalf("row = %q, want a lower then an upper block, with no background", row)
	}

	empty := filepath.Join(t.TempDir(), "empty.png")
	writeImage(t, empty, 4, 4, func(x, y int) color.Color { return transparent })
	p = LoadPreviewWithConfig(fileInfo(t, empty), drawConfig(ImagesTrueColor))
	if row := p.Image.Rows(4, 2)[0]; row != "    " {
		t.Fatalf("clear image row = %q, want plain spaces", row)
	}
}

func TestImageFormats(t *testing.T) {
	dir := t.TempDir()
	for ext, kind := range map[string]string{".png": "PNG Image", ".jpg": "JPEG Image", ".gif": "GIF Image", ".bmp": "BMP Image"} {
		path := filepath.Join(dir, "picture"+ext)
		writeImage(t, path, 30, 10, stripes)
		p := LoadPreviewWithConfig(fileInfo(t, path), drawConfig(ImagesTrueColor))
		if p.Image == nil || p.Kind != kind || strings.Join(p.Details, " ") != "30x10" {
			t.Errorf("%s: Image=%v Kind=%q Details=%v: %v", ext, p.Image != nil, p.Kind, p.Details, p.Lines)
		}
	}

	// A 1x1 lossless WebP; the standard library has no encoder
	webp, _ := base64.StdEncoding.DecodeString("UklGRhoAAABXRUJQVlA4TA0AAAAvAAAAEAcQERGIiP4HAA==")
	path := filepath.Join(dir, "pixel.webp")
	os.WriteFile(path, webp, 0644)
	if p := LoadPreviewWithConfig(fileInfo(t, path), drawConfig(ImagesTrueColor)); p.Image == nil || p.Kind != "WebP Image" {
		t.Errorf("webp: Image=%v Kind=%q: %v", p.Image != nil, p.Kind, p.Lines)
	}
}

func TestImagesThatAreNotDrawn(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "photo.png")
	writeImage(t, path, 40, 20, stripes)

	// Without colors (or with --ascii) the details show, with the size
	p := LoadPreviewWithConfig(fileInfo(t, path), drawConfig(ImagesOff))
	view := strings.Join(render(t, p, 80, 6, 0), "\n")
	if p.Image != nil || !strings.Contains(view, "40x20") || !strings.Contains(view, "Modified") || !strings.Contains(view, "256 colors") {
		t.Fatalf("image drawn=%v:\n%s", p.Image != nil, view)
	}

	// At startup the decoding is left for later
	quick := drawConfig(ImagesTrueColor)
	quick.Quick = true
	if p := LoadPreviewWithConfig(fileInfo(t, path), quick); !p.Pending || p.Image != nil {
		t.Fatalf("quick load: Pending=%v Image=%v", p.Pending, p.Image != nil)
	}

	// Huge images are refused from their header, before any decoding
	old := maxImagePixels
	maxImagePixels = 100
	defer func() { maxImagePixels = old }()
	p = LoadPreviewWithConfig(fileInfo(t, path), drawConfig(ImagesTrueColor))
	if p.Image != nil || !strings.Contains(p.Content, "Too large to draw") {
		t.Fatalf("over the limit: Image=%v %q", p.Image != nil, p.Content)
	}

	broken := filepath.Join(dir, "broken.png")
	os.WriteFile(broken, []byte("\x89PNG not really"), 0644)
	if p := LoadPreviewWithConfig(fileInfo(t, broken), drawConfig(ImagesTrueColor)); p.Image != nil || !strings.Contains(p.Content, "Cannot preview image") {
		t.Fatalf("broken image: %q", p.Content)
	}
}

func TestImageRedrawsOnlyForANewSize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "photo.png")
	writeImage(t, path, 120, 80, stripes)
	img := LoadPreviewWithConfig(fileInfo(t, path), drawConfig(ImagesTrueColor)).Image

	small := img.Rows(20, 5)
	if again := img.Rows(20, 5); &again[0] != &small[0] {
		t.Fatal("the same size was drawn again")
	}
	large := img.Rows(60, 20)
	for i, rows := range [][]string{small, large} {
		width := []int{20, 60}[i]
		for _, row := range rows {
			if w := utils.Width(row); w != width {
				t.Fatalf("row is %d wide, want %d", w, width)
			}
		}
	}
	if strings.Count(large[0], "▀") <= strings.Count(small[0], "▀") {
		t.Fatal("a larger pane should draw a larger image")
	}
}

func TestDecodedImagesAreCached(t *testing.T) {
	path := filepath.Join(t.TempDir(), "photo.png")
	writeImage(t, path, 30, 30, stripes)
	first := LoadPreviewWithConfig(fileInfo(t, path), drawConfig(ImagesTrueColor)).Image
	if again := LoadPreviewWithConfig(fileInfo(t, path), drawConfig(ImagesTrueColor)).Image; again != first {
		t.Fatal("the same file was decoded twice")
	}

	// A changed file is decoded again
	writeImage(t, path, 50, 10, stripes)
	if changed := LoadPreviewWithConfig(fileInfo(t, path), drawConfig(ImagesTrueColor)).Image; changed == first || changed.Width != 50 {
		t.Fatalf("changed file: same=%v width=%d", changed == first, changed.Width)
	}
}

func TestDetectImageColors(t *testing.T) {
	oldProfile, oldMode := lipgloss.ColorProfile(), ui.GetIconMode()
	defer func() {
		lipgloss.SetColorProfile(oldProfile)
		ui.SetIconMode(oldMode)
	}()

	ui.SetIconMode(ui.IconModeNerd)
	for profile, want := range map[termenv.Profile]ImageColors{
		termenv.TrueColor: ImagesTrueColor,
		termenv.ANSI256:   Images256,
		termenv.ANSI:      ImagesOff,
		termenv.Ascii:     ImagesOff,
	} {
		lipgloss.SetColorProfile(profile)
		if got := DetectImageColors(); got != want {
			t.Errorf("profile %v: %v, want %v", profile, got, want)
		}
	}

	lipgloss.SetColorProfile(termenv.TrueColor)
	ui.SetIconMode(ui.IconModeASCII)
	if got := DetectImageColors(); got != ImagesOff {
		t.Errorf("--ascii: %v, want images off", got)
	}
}

func TestXterm256(t *testing.T) {
	for c, want := range map[color.NRGBA]int{
		{255, 0, 0, 255}:     196,
		{0, 0, 0, 255}:       16,
		{255, 255, 255, 255}: 231,
		{128, 128, 128, 255}: 244, // The gray ramp beats the cube's 102
		{95, 135, 175, 255}:  67,
	} {
		if got := xterm256(c); got != want {
			t.Errorf("xterm256(%v) = %d, want %d", c, got, want)
		}
	}
}
