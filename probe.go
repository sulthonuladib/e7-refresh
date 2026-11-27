package main

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"os"
	"path/filepath"

	"github.com/sulthonuladib/e7-refresh/internal/assets"
	"github.com/sulthonuladib/e7-refresh/internal/config"
	"github.com/sulthonuladib/e7-refresh/internal/platform"
	"github.com/sulthonuladib/e7-refresh/internal/vision"
)

// probeResult is one line of `e7-refresh probe`, printed as text or JSON.
type probeResult struct {
	Item    string  `json:"item"`
	Name    string  `json:"name"`
	Enabled bool    `json:"enabled"`
	Score   float64 `json:"score"`
	X       int     `json:"x"`
	Y       int     `json:"y"`
	Match   bool    `json:"match"`
	W       int     `json:"-"`
	H       int     `json:"-"`
}

// runProbe captures the game window (or reads a screenshot) and reports how
// well each bookmark template matches. It never clicks or refreshes, so it is
// the safe way to check "does it see this bookmark" when the shop shows a
// covenant or mystic item.
func runProbe(args []string) {
	cfg, err := config.Load()
	if err != nil {
		fail(err)
	}
	cfg.Normalize()

	var frame image.Image
	var rect platform.Rect
	if shot := flag(args, "--shot"); shot != "" {
		frame, err = readImage(shot)
		if err != nil {
			fail(err)
		}
		b := frame.Bounds()
		rect = platform.Rect{X: b.Min.X, Y: b.Min.Y, W: b.Dx(), H: b.Dy()}
	} else {
		// Probe only needs pixels and window geometry; it never sends a
		// click, so a missing input tool is not a reason to refuse.
		desktop := platform.New()
		window, err := desktop.FindWindow(cfg.WindowTitle)
		if err != nil {
			fail(err)
		}
		if err := desktop.Activate(window); err != nil {
			fail(fmt.Errorf("activate window: %w", err))
		}
		if !cfg.AllowMove {
			window.Rect = platform.Rect{X: 0, Y: 0, W: cfg.WindowWidth, H: cfg.WindowHeight}
		}
		if err := desktop.MoveResize(window, window.Rect); err != nil {
			fail(fmt.Errorf("move window: %w", err))
		}
		if actual, err := desktop.Geometry(window); err == nil && actual.Valid() {
			window.Rect = actual
		}
		rect = window.Rect
		frame, err = desktop.Capture()
		if err != nil {
			fail(fmt.Errorf("screenshot: %w", err))
		}
	}

	bounds := frame.Bounds()
	box := image.Rect(rect.X, rect.Y, rect.X+rect.W, rect.Y+rect.H)
	if !box.In(bounds) {
		fail(fmt.Errorf("window %v is outside the captured screen %v", box, bounds))
	}
	cropped := cropImage(frame, box)
	templateWidth := cfg.TemplateWidth
	screen := vision.Prepare(cropped, templateWidth)

	results := make([]probeResult, 0, len(config.Items))
	matched := map[string]bool{}
	for _, item := range config.Items {
		var path string
		var err error
		if cfg.TemplateDir != "" {
			path = filepath.Join(cfg.TemplateDir, item.File)
			if _, statErr := os.Stat(path); statErr != nil {
				fail(fmt.Errorf("custom template %s: %w", path, statErr))
			}
		} else {
			path, err = assets.Pattern("assets", item.File)
			if err != nil {
				fail(err)
			}
		}
		template, err := vision.LoadTemplate(item.ID, path)
		if err != nil {
			fail(err)
		}
		x, y, score, ok := screen.MatchBest(template, cfg.MatchThreshold)
		// Report a hit only for enabled items, but still score the others so
		// a false positive is visible in the output.
		enabled := cfg.Items[item.ID]
		results = append(results, probeResult{
			Item: item.ID, Name: item.Name, Enabled: enabled,
			Score: math.Round(score*10000) / 10000,
			X:     x, Y: y, Match: ok,
			W: template.W, H: template.H,
		})
		if ok {
			matched[item.ID] = true
		}
	}

	save := flag(args, "--save")
	if save == "" {
		save = filepath.Join(config.DataDir(), "probe.png")
	}
	if err := saveProbe(save, cropped, screen, results, templateWidth); err != nil {
		fmt.Fprintf(os.Stderr, "e7-refresh: save probe image: %v\n", err)
	}

	if hasFlag(args, "--json") {
		if err := printJSON(map[string]any{
			"window": map[string]int{
				"x": rect.X, "y": rect.Y, "width": rect.W, "height": rect.H,
			},
			"templateWidth": templateWidth,
			"threshold":     cfg.MatchThreshold,
			"items":         results,
			"image":         save,
		}); err != nil {
			fail(err)
		}
		return
	}
	fmt.Printf("window %dx%d at %d,%d · templates at %dpx · threshold %.2f\n",
		rect.W, rect.H, rect.X, rect.Y, templateWidth, cfg.MatchThreshold)
	for _, r := range results {
		state := "no match"
		if r.Match {
			state = "MATCH"
		}
		if !r.Enabled {
			state += " (disabled)"
		}
		// Convert the reference-pixel position back to screen pixels so the
		// number can be compared with a screenshot.
		k := float64(rect.W) / float64(templateWidth)
		fmt.Printf("  %-12s %-8s score %.3f at %d,%d (window %d,%d)\n",
			r.Name, state, r.Score, r.X, r.Y,
			rect.X+int(math.Round(float64(r.X)*k)),
			rect.Y+int(math.Round(float64(r.Y)*k)))
	}
	fmt.Printf("annotated image: %s\n", save)
}

func readImage(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	return img, nil
}

func cropImage(img image.Image, box image.Rectangle) image.Image {
	out := image.NewRGBA(image.Rect(0, 0, box.Dx(), box.Dy()))
	draw.Draw(out, out.Bounds(), img, box.Min, draw.Src)
	return out
}

// saveProbe writes the captured window with a box around each item, so the
// user can see what the matcher saw without reading the scores.
func saveProbe(path string, frame image.Image, screen *vision.Screen, results []probeResult, templateWidth int) error {
	b := frame.Bounds()
	out := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(out, out.Bounds(), frame, b.Min, draw.Src)
	k := float64(b.Dx()) / float64(templateWidth)
	for _, r := range results {
		colour := probeColour(r.Item)
		if !r.Match {
			colour = color.RGBA{R: 90, G: 90, B: 96, A: 255}
		}
		box := image.Rect(
			int(math.Round(float64(r.X)*k)), int(math.Round(float64(r.Y)*k)),
			int(math.Round(float64(r.X+r.W)*k)), int(math.Round(float64(r.Y+r.H)*k)),
		)
		for i := 0; i < 2; i++ {
			drawBox(out, box, i, colour)
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, out)
}

func drawBox(dst *image.RGBA, box image.Rectangle, inset int, c color.Color) {
	top := image.Rect(box.Min.X-inset, box.Min.Y-inset, box.Max.X+inset, box.Min.Y-inset+1)
	bottom := image.Rect(box.Min.X-inset, box.Max.Y+inset-1, box.Max.X+inset, box.Max.Y+inset)
	left := image.Rect(box.Min.X-inset, box.Min.Y-inset, box.Min.X-inset+1, box.Max.Y+inset)
	right := image.Rect(box.Max.X+inset-1, box.Min.Y-inset, box.Max.X+inset, box.Max.Y+inset)
	for _, r := range []image.Rectangle{top, bottom, left, right} {
		draw.Draw(dst, r, &image.Uniform{C: c}, image.Point{}, draw.Src)
	}
}

func probeColour(id string) color.RGBA {
	switch id {
	case "cov":
		return color.RGBA{R: 94, G: 165, B: 255, A: 255}
	case "mys":
		return color.RGBA{R: 255, G: 106, B: 106, A: 255}
	default:
		return color.RGBA{R: 196, G: 160, B: 110, A: 255}
	}
}
