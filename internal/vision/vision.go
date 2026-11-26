// Package vision finds shop items in a screenshot by normalized template
// matching, the same statistic OpenCV's TM_CCOEFF_NORMED uses in the
// upstream application. It is pure Go, so the daemon needs no cgo or OpenCV.
//
// A full-resolution search over a 1920x1080 frame for every item is too slow
// in plain Go, so matching is coarse-to-fine: a quarter-scale pass finds
// candidates with integral images, then a full-resolution pass scores the
// few candidates around each hit.
package vision

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"sort"
)

// Scale is the downsampling factor of the coarse search.
const Scale = 4

// Template is a grayscale item image prepared for matching.
type Template struct {
	Name  string
	Gray  []uint8
	W, H  int
	coarse []uint8
	cw, ch int
	meanC float64
	varC  float64
	meanF float64
	varF  float64
}

// LoadTemplate decodes a PNG template from disk.
func LoadTemplate(name, path string) (*Template, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	return NewTemplate(name, img), nil
}

// NewTemplate prepares an image for repeated matching.
func NewTemplate(name string, img image.Image) *Template {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	gray := make([]uint8, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			gray[y*w+x] = grayPixel(img, b.Min.X+x, b.Min.Y+y)
		}
	}
	t := &Template{Name: name, Gray: gray, W: w, H: h}
	t.meanF, t.varF = stats(gray, w, h)
	t.coarse, t.cw, t.ch = downsample(gray, w, h, Scale)
	t.meanC, t.varC = stats(t.coarse, t.cw, t.ch)
	return t
}

// Screen is a prepared frame. Prepare it once per screenshot and match all
// templates against it.
type Screen struct {
	gray        []uint8
	w, h        int
	coarse      []uint8
	cw, ch      int
	sum, sumSq  []int64
	sumW, sumH  int
}

// Prepare converts a screenshot to grayscale and builds the coarse integral
// images used by the first pass.
func Prepare(img image.Image) *Screen {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	gray := make([]uint8, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			gray[y*w+x] = grayPixel(img, b.Min.X+x, b.Min.Y+y)
		}
	}
	coarse, cw, ch := downsample(gray, w, h, Scale)
	sw, sh := cw+1, ch+1
	sum := make([]int64, sw*sh)
	sumSq := make([]int64, sw*sh)
	for y := 0; y < ch; y++ {
		var row, rowSq int64
		for x := 0; x < cw; x++ {
			v := int64(coarse[y*cw+x])
			row += v
			rowSq += v * v
			sum[(y+1)*sw+x+1] = sum[y*sw+x+1] + row
			sumSq[(y+1)*sw+x+1] = sumSq[y*sw+x+1] + rowSq
		}
	}
	return &Screen{gray: gray, w: w, h: h, coarse: coarse, cw: cw, ch: ch,
		sum: sum, sumSq: sumSq, sumW: sw, sumH: sh}
}

// Match returns the top-left position and score of the best occurrence of t.
// Positions are relative to the screenshot, with (0,0) at its top-left.
func (s *Screen) Match(t *Template, threshold float64) (int, int, float64, bool) {
	if t == nil || s.cw < t.cw || s.ch < t.ch {
		return 0, 0, 0, false
	}
	type hit struct {
		x, y int
		score float64
	}
	var hits []hit
	n := t.cw * t.ch
	for y := 0; y+t.ch <= s.ch; y++ {
		for x := 0; x+t.cw <= s.cw; x++ {
			sumS := s.rectSum(s.sum, x, y, t.cw, t.ch)
			sumSqS := s.rectSum(s.sumSq, x, y, t.cw, t.ch)
			meanS := float64(sumS) / float64(n)
			varS := float64(sumSqS)/float64(n) - meanS*meanS
			if varS <= 0 || t.varC <= 0 {
				continue
			}
			var cross int64
			for ty := 0; ty < t.ch; ty++ {
				rowS := (y+ty)*s.cw + x
				rowT := ty * t.cw
				for tx := 0; tx < t.cw; tx++ {
					cross += int64(s.coarse[rowS+tx]) * int64(t.coarse[rowT+tx])
				}
			}
			cov := float64(cross)/float64(n) - meanS*t.meanC
			score := cov / math.Sqrt(varS*t.varC)
			if score >= threshold*0.85 {
				hits = append(hits, hit{x * Scale, y * Scale, score})
			}
		}
	}
	if len(hits) == 0 {
		return 0, 0, 0, false
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].score > hits[j].score })

	bestX, bestY, bestScore := 0, 0, -1.0
	for _, candidate := range hits[:min(12, len(hits))] {
		x, y, score, ok := s.refine(candidate.x, candidate.y, t)
		if ok && score > bestScore {
			bestX, bestY, bestScore = x, y, score
		}
	}
	return bestX, bestY, bestScore, bestScore >= threshold
}

// refine scores the neighborhood of a coarse hit at full resolution.
func (s *Screen) refine(cx, cy int, t *Template) (int, int, float64, bool) {
	radius := 2 * Scale
	x0 := max(0, cx-radius)
	y0 := max(0, cy-radius)
	x1 := min(s.w-t.W, cx+radius)
	y1 := min(s.h-t.H, cy+radius)
	if x1 < x0 || y1 < y0 {
		return 0, 0, 0, false
	}
	n := t.W * t.H
	bestX, bestY, best := x0, y0, -1.0
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			var sumS, sumSqS, cross int64
			for ty := 0; ty < t.H; ty++ {
				rowS := (y+ty)*s.w + x
				rowT := ty * t.W
				for tx := 0; tx < t.W; tx++ {
					v := int64(s.gray[rowS+tx])
					sumS += v
					sumSqS += v * v
					cross += v * int64(t.Gray[rowT+tx])
				}
			}
			meanS := float64(sumS) / float64(n)
			varS := float64(sumSqS)/float64(n) - meanS*meanS
			if varS <= 0 || t.varF <= 0 {
				continue
			}
			score := (float64(cross)/float64(n) - meanS*t.meanF) / math.Sqrt(varS*t.varF)
			if score > best {
				best, bestX, bestY = score, x, y
			}
		}
	}
	return bestX, bestY, best, true
}

func (s *Screen) rectSum(m []int64, x, y, w, h int) int64 {
	return m[(y+h)*s.sumW+x+w] - m[y*s.sumW+x+w] - m[(y+h)*s.sumW+x] + m[y*s.sumW+x]
}

func grayPixel(img image.Image, x, y int) uint8 {
	switch m := img.(type) {
	case *image.Gray:
		return m.Pix[(y-m.Rect.Min.Y)*m.Stride+(x-m.Rect.Min.X)]
	case *image.RGBA:
		i := (y-m.Rect.Min.Y)*m.Stride + (x-m.Rect.Min.X)*4
		// Rec. 601 luma, close enough to the conversions OpenCV uses here.
		return uint8((int(m.Pix[i])*299 + int(m.Pix[i+1])*587 + int(m.Pix[i+2])*114) / 1000)
	default:
		r, g, b, _ := img.At(x, y).RGBA()
		return uint8((int(r>>8)*299 + int(g>>8)*587 + int(b>>8)*114) / 1000)
	}
}

func stats(p []uint8, w, h int) (float64, float64) {
	n := float64(w * h)
	var sum, sq int64
	for _, v := range p {
		sum += int64(v)
		sq += int64(v) * int64(v)
	}
	mean := float64(sum) / n
	return mean, float64(sq)/n - mean*mean
}

func downsample(gray []uint8, w, h, scale int) ([]uint8, int, int) {
	cw, ch := w/scale, h/scale
	if cw == 0 || ch == 0 {
		return nil, 0, 0
	}
	out := make([]uint8, cw*ch)
	for y := 0; y < ch; y++ {
		for x := 0; x < cw; x++ {
			var sum int
			for dy := 0; dy < scale; dy++ {
				row := (y*scale+dy)*w + x*scale
				for dx := 0; dx < scale; dx++ {
					sum += int(gray[row+dx])
				}
			}
			out[y*cw+x] = uint8(sum / (scale * scale))
		}
	}
	return out, cw, ch
}

// Keep color imported so callers can pass any image.Image, including paletted
// PNGs decoded through color models.
var _ color.Color = color.Gray{}
