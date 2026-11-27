package vision

import (
	"image"
	"image/png"
	"os"
	"testing"

	xdraw "golang.org/x/image/draw"
)

func loadPNG(t *testing.T, path string) image.Image {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	return img
}

// scene pastes each item at the given top-left corner on a dark frame the
// size of the upstream capture. Pasting the raw colour with full alpha makes
// the pasted region identical to the template, so a correct matcher finds it
// at (x, y) with a score near 1.
func scene(t *testing.T, width, height int, placements map[string]image.Point) image.Image {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			i := y*img.Stride + x*4
			img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = 18, 18, 24, 255
		}
	}
	for name, at := range placements {
		src := loadPNG(t, "../../resources/assets/"+name+".png")
		b := src.Bounds()
		for y := 0; y < b.Dy(); y++ {
			for x := 0; x < b.Dx(); x++ {
				r, g, bl := rawRGB(src, b.Min.X+x, b.Min.Y+y)
				i := (at.Y+y)*img.Stride + (at.X+x)*4
				img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = r, g, bl, 255
			}
		}
	}
	return img
}

func TestMatchFindsEachColourAtNativeScale(t *testing.T) {
	placements := map[string]image.Point{
		"cov": {80, 60},
		"mys": {400, 60},
		"fb":  {700, 60},
	}
	s := Prepare(scene(t, ReferenceWidth, 510, placements), ReferenceWidth)
	for name, want := range placements {
		tpl := NewTemplate(name, loadPNG(t, "../../resources/assets/"+name+".png"))
		x, y, score, ok := s.MatchBest(tpl, 0.70)
		if !ok {
			t.Fatalf("%s: not found (score %.3f)", name, score)
		}
		if x != want.X || y != want.Y {
			t.Errorf("%s: found at %d,%d want %d,%d (score %.3f)", name, x, y, want.X, want.Y, score)
		}
	}
}

func TestMatchIsColourAware(t *testing.T) {
	// Only friendship is present; covenant shares its silhouette but not its
	// colour, so it must stay below the threshold. Grayscale matching scores
	// this frame at 0.61, which is a false positive at the default 0.62.
	s := Prepare(scene(t, ReferenceWidth, 510, map[string]image.Point{"fb": {300, 200}}), ReferenceWidth)
	cov := NewTemplate("cov", loadPNG(t, "../../resources/assets/cov.png"))
	if _, _, score, ok := s.MatchBest(cov, 0.62); ok {
		t.Fatalf("covenant falsely matched friendship at score %.3f", score)
	}
	fb := NewTemplate("fb", loadPNG(t, "../../resources/assets/fb.png"))
	if _, _, score, ok := s.MatchBest(fb, 0.70); !ok {
		t.Fatalf("friendship not found: score %.3f", score)
	}
}

func TestPrepareNormalisesLargerWindows(t *testing.T) {
	placements := map[string]image.Point{"cov": {80, 60}, "mys": {400, 60}, "fb": {700, 60}}
	native := scene(t, ReferenceWidth, 510, placements)
	// A 16:9 game window at 1280x720 renders the same capture 1.4127x bigger.
	big := image.NewRGBA(image.Rect(0, 0, 1280, 720))
	xdraw.CatmullRom.Scale(big, big.Bounds(), native, native.Bounds(), xdraw.Src, nil)
	s := Prepare(big, ReferenceWidth)
	for name, at := range placements {
		tpl := NewTemplate(name, loadPNG(t, "../../resources/assets/"+name+".png"))
		x, y, score, ok := s.MatchBest(tpl, 0.70)
		if !ok {
			t.Fatalf("%s: not found in scaled frame (score %.3f)", name, score)
		}
		// Prepare returns positions in reference-width pixels, so they map
		// back to the same place on the 906-wide capture.
		if abs(x-at.X) > 1 || abs(y-at.Y) > 1 {
			t.Errorf("%s: found at %d,%d want ~%d,%d (score %.3f)", name, x, y, at.X, at.Y, score)
		}
	}
}

// TestDiag scores a real screenshot supplied with SHOT, for debugging
// "the app does not see this bookmark" reports without running a refresh.
func TestDiag(t *testing.T) {
	path := os.Getenv("SHOT")
	if path == "" {
		t.Skip("set SHOT=/path/to/screenshot.png")
	}
	img := loadPNG(t, path)
	b := img.Bounds()
	width := ReferenceWidth
	if v := os.Getenv("SHOT_WIDTH"); v != "" {
		var n int
		for _, c := range v {
			if c < '0' || c > '9' {
				break
			}
			n = n*10 + int(c-'0')
		}
		if n > 0 {
			width = n
		}
	}
	s := Prepare(img, width)
	t.Logf("frame %dx%d normalised to %dx%d", b.Dx(), b.Dy(), s.W, s.H)
	for _, name := range []string{"cov", "mys", "fb"} {
		tpl := NewTemplate(name, loadPNG(t, "../../resources/assets/"+name+".png"))
		x, y, score, _ := s.MatchBest(tpl, 0.0)
		t.Logf("%s: score=%.4f at %d,%d threshold_ok=%v", name, score, x, y, score >= 0.62)
	}
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
