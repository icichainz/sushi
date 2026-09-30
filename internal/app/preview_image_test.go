package app

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func TestImagePreviewFollowsTheTerminalSize(t *testing.T) {
	// Images are drawn only where there are colors to draw them with
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(old)

	dir := t.TempDir()
	img := image.NewNRGBA(image.Rect(0, 0, 64, 48))
	for y := 0; y < 48; y++ {
		for x := 0; x < 64; x++ {
			img.Set(x, y, color.NRGBA{uint8(x * 4), uint8(y * 5), 128, 255})
		}
	}
	f, err := os.Create(filepath.Join(dir, "photo.png"))
	if err != nil {
		t.Fatal(err)
	}
	png.Encode(f, img)
	f.Close()

	// Startup leaves the decoding to Init, so it doesn't hold things up
	m := newTestModel(t, dir, nil)
	if !m.tab().Preview.Pending {
		t.Fatal("the first preview decoded the image before startup finished")
	}
	m = drain(t, m, m.Init())
	if m.tab().Preview.Image == nil {
		t.Fatalf("no image after Init: %v", m.tab().Preview.Lines)
	}

	drawn := map[int]int{}
	for _, size := range sizes {
		m = resize(m, size)
		label := fmt.Sprintf("%dx%d", size.Width, size.Height)
		lines := assertFills(t, label, m)
		cells := strings.Count(strings.Join(lines, "\n"), "▀")
		if (m.layout().previewW > 0) != (cells > 0) {
			t.Fatalf("%s: %d cells of image with the preview shown=%v", label, cells, m.layout().previewW > 0)
		}
		drawn[size.Width*1000+size.Height] = cells
	}
	if drawn[140040] <= drawn[100024] || drawn[100024] <= drawn[80024] {
		t.Fatalf("the image should shrink with the terminal: %v", drawn)
	}
}
