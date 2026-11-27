// Package vision finds shop items in a screenshot by normalized template
// matching, the same statistic OpenCV's TM_CCOEFF_NORMED uses in the
// upstream application. It is pure Go, so the daemon needs no cgo or OpenCV.
//
// The upstream application matches in colour, not grayscale: covenant,
// mystic and friendship bookmarks share a silhouette, so a grayscale match
// confuses them. This package matches all three colour channels.
//
// Matching is coarse-to-fine. A quarter-scale grayscale pass finds candidate
// positions with integral images, then a full-resolution colour pass scores
// the few candidates around each hit. The screenshot is first scaled to the
// pixel width the templates were captured at, so a window of any size matches
// at the template's native scale instead of being blurred by resampling the
// templates.
package vision

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"sort"

	xdraw "golang.org/x/image/draw"
)

// Scale is the downsampling factor of the coarse search.
const Scale = 4

// ReferenceWidth is the width of the capture the shipped mouse-mode
// templates were cropped from. The upstream app resized the game window to
// 906x539 (a 16:9 client area of 906x510), so templates are native at 906.
const ReferenceWidth = 906

// Template is a colour item image prepared for matching.
type Template struct {
	Name string
	// R, G and B are the raw channels of the template, row-major. Alpha is
	// ignored, as OpenCV's imread ignores it.
	R, G, B []uint8
	W, H    int

	gray   []uint8
	coarse []uint8
	cw, ch int

	sumR, sumG, sumB float64
	sum2             float64
	meanC            float64
	varC             float64
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
	t := &Template{
		Name: name, W: w, H: h,
		R: make([]uint8, w*h), G: make([]uint8, w*h), B: make([]uint8, w*h),
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r, g, bl := rawRGB(img, b.Min.X+x, b.Min.Y+y)
			i := y*w + x
			t.R[i], t.G[i], t.B[i] = r, g, bl
			t.sumR += float64(r)
			t.sumG += float64(g)
			t.sumB += float64(bl)
			t.sum2 += float64(int(r)*int(r) + int(g)*int(g) + int(bl)*int(bl))
		}
	}
	t.finish()
	return t
}

func (t *Template) finish() {
	t.gray = make([]uint8, t.W*t.H)
	for i := range t.gray {
		t.gray[i] = luma(t.R[i], t.G[i], t.B[i])
	}
	t.coarse, t.cw, t.ch = downsample(t.gray, t.W, t.H, Scale)
	t.meanC, t.varC = grayStats(t.coarse)
}

// Scaled returns a copy of the template resized by factor with a smooth
// filter, for the rare case where the screenshot cannot be normalised to the
// template's reference width.
func (t *Template) Scaled(factor float64) *Template {
	w := max(1, int(math.Round(float64(t.W)*factor)))
	h := max(1, int(math.Round(float64(t.H)*factor)))
	src := t.image()
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), xdraw.Src, nil)
	return NewTemplate(t.Name, dst)
}

// image returns the template as an opaque RGBA image for resampling.
func (t *Template) image() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, t.W, t.H))
	for y := 0; y < t.H; y++ {
		for x := 0; x < t.W; x++ {
			i := y*t.W + x
			j := y*img.Stride + x*4
			img.Pix[j+0] = t.R[i]
			img.Pix[j+1] = t.G[i]
			img.Pix[j+2] = t.B[i]
			img.Pix[j+3] = 255
		}
	}
	return img
}

// Screen is a prepared frame. Prepare it once per screenshot and match all
// templates against it.
type Screen struct {
	R, G, B []uint8
	W, H    int

	gray   []uint8
	coarse []uint8
	cw, ch int

	sum, sumSq []int64
	sumW, sumH int
}

// Prepare converts a screenshot to a colour frame scaled to width, which
// should be the width the templates were captured at. A width of zero uses
// ReferenceWidth. Scaling the frame once keeps every template at its native
// size, which is both faster and more accurate than resampling each template
// for every screenshot.
func Prepare(img image.Image, width int) *Screen {
	if width <= 0 {
		width = ReferenceWidth
	}
	b := img.Bounds()
	srcW, srcH := b.Dx(), b.Dy()
	if srcW <= 0 || srcH <= 0 {
		return &Screen{}
	}
	height := max(1, int(math.Round(float64(srcH)*float64(width)/float64(srcW))))
	rgba := image.NewRGBA(image.Rect(0, 0, width, height))
	if srcW == width {
		xdraw.Draw(rgba, rgba.Bounds(), img, b.Min, xdraw.Src)
	} else {
		xdraw.CatmullRom.Scale(rgba, rgba.Bounds(), img, b, xdraw.Src, nil)
	}
	s := &Screen{
		W: width, H: height,
		R: make([]uint8, width*height),
		G: make([]uint8, width*height),
		B: make([]uint8, width*height),
	}
	for y := 0; y < height; y++ {
		row := y * rgba.Stride
		for x := 0; x < width; x++ {
			i := y*width + x
			j := row + x*4
			s.R[i], s.G[i], s.B[i] = rgba.Pix[j], rgba.Pix[j+1], rgba.Pix[j+2]
		}
	}
	s.gray = make([]uint8, width*height)
	for i := range s.gray {
		s.gray[i] = luma(s.R[i], s.G[i], s.B[i])
	}
	s.coarse, s.cw, s.ch = downsample(s.gray, width, height, Scale)
	sw, sh := s.cw+1, s.ch+1
	s.sum = make([]int64, sw*sh)
	s.sumSq = make([]int64, sw*sh)
	for y := 0; y < s.ch; y++ {
		var row, rowSq int64
		for x := 0; x < s.cw; x++ {
			v := int64(s.coarse[y*s.cw+x])
			row += v
			rowSq += v * v
			s.sum[(y+1)*sw+x+1] = s.sum[y*sw+x+1] + row
			s.sumSq[(y+1)*sw+x+1] = s.sumSq[y*sw+x+1] + rowSq
		}
	}
	s.sumW, s.sumH = sw, sh
	return s
}

// MatchBest returns the strongest hit. Because the frame is normalised to the
// template width, the native scale is tried first; a couple of slightly
// smaller and larger scales cover a client that letterboxes its content.
func (s *Screen) MatchBest(t *Template, threshold float64) (int, int, float64, bool) {
	if s == nil || s.W == 0 {
		return 0, 0, 0, false
	}
	if x, y, score, ok := s.Match(t, threshold); ok {
		return x, y, score, true
	}
	bestX, bestY, bestScore := 0, 0, 0.0
	for _, factor := range []float64{0.96, 1.04} {
		x, y, score, ok := s.Match(t.Scaled(factor), threshold)
		if ok && score > bestScore {
			bestX, bestY, bestScore = x, y, score
		}
	}
	return bestX, bestY, bestScore, bestScore >= threshold
}

// Match returns the top-left position and score of the best occurrence of t.
// Positions are relative to the prepared frame, with (0,0) at its top-left.
func (s *Screen) Match(t *Template, threshold float64) (int, int, float64, bool) {
	if t == nil || t.cw == 0 || t.ch == 0 || s.cw < t.cw || s.ch < t.ch {
		return 0, 0, 0, false
	}
	type hit struct {
		x, y  int
		score float64
	}
	var hits []hit
	n := float64(t.cw * t.ch)
	for y := 0; y+t.ch <= s.ch; y++ {
		for x := 0; x+t.cw <= s.cw; x++ {
			sumS := s.rectSum(s.sum, x, y, t.cw, t.ch)
			sumSqS := s.rectSum(s.sumSq, x, y, t.cw, t.ch)
			meanS := float64(sumS) / n
			varS := float64(sumSqS)/n - meanS*meanS
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
			cov := float64(cross)/n - meanS*t.meanC
			score := cov / math.Sqrt(varS*t.varC)
			// The colour pass is stricter than the grayscale pass, so keep a
			// margin here or a real icon can be filtered out before it is
			// scored in colour.
			if score >= threshold*0.75 {
				hits = append(hits, hit{x * Scale, y * Scale, score})
			}
		}
	}
	if len(hits) == 0 {
		return 0, 0, 0, false
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].score > hits[j].score })

	bestX, bestY, bestScore := 0, 0, -1.0
	for _, candidate := range hits[:min(8, len(hits))] {
		x, y, score, ok := s.refine(candidate.x, candidate.y, t)
		if ok && score > bestScore {
			bestX, bestY, bestScore = x, y, score
		}
	}
	return bestX, bestY, bestScore, bestScore >= threshold
}

// refine scores the neighbourhood of a coarse hit at full resolution in
// colour, the statistic the upstream application uses.
func (s *Screen) refine(cx, cy int, t *Template) (int, int, float64, bool) {
	radius := Scale
	x0 := max(0, cx-radius)
	y0 := max(0, cy-radius)
	x1 := min(s.W-t.W, cx+radius)
	y1 := min(s.H-t.H, cy+radius)
	if x1 < x0 || y1 < y0 {
		return 0, 0, 0, false
	}
	bestX, bestY, best := x0, y0, -1.0
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			score, ok := s.colorScore(x, y, t)
			if !ok {
				continue
			}
			if score > best {
				best, bestX, bestY = score, x, y
			}
		}
	}
	return bestX, bestY, best, true
}

// colorScore is OpenCV's TM_CCOEFF_NORMED over the three colour channels,
// each centred on its own mean, which is what upstream matchTemplate does
// with a BGR/RGB template.
func (s *Screen) colorScore(x, y int, t *Template) (float64, bool) {
	n := float64(t.W * t.H)
	var sumSR, sumSG, sumSB float64
	var sumS2, cross float64
	for ty := 0; ty < t.H; ty++ {
		rowS := (y+ty)*s.W + x
		rowT := ty * t.W
		for tx := 0; tx < t.W; tx++ {
			sr, sg, sb := s.R[rowS+tx], s.G[rowS+tx], s.B[rowS+tx]
			tr, tg, tb := t.R[rowT+tx], t.G[rowT+tx], t.B[rowT+tx]
			sumSR += float64(sr)
			sumSG += float64(sg)
			sumSB += float64(sb)
			sumS2 += float64(int(sr)*int(sr) + int(sg)*int(sg) + int(sb)*int(sb))
			cross += float64(int(sr)*int(tr) + int(sg)*int(tg) + int(sb)*int(tb))
		}
	}
	numerator := cross - (sumSR*t.sumR+sumSG*t.sumG+sumSB*t.sumB)/n
	varS := sumS2 - (sumSR*sumSR+sumSG*sumSG+sumSB*sumSB)/n
	varT := t.sum2 - (t.sumR*t.sumR+t.sumG*t.sumG+t.sumB*t.sumB)/n
	if varS <= 0 || varT <= 0 {
		return 0, false
	}
	return numerator / math.Sqrt(varS*varT), true
}

func (s *Screen) rectSum(m []int64, x, y, w, h int) int64 {
	return m[(y+h)*s.sumW+x+w] - m[y*s.sumW+x+w] - m[(y+h)*s.sumW+x] + m[y*s.sumW+x]
}

// rawRGB returns the un-premultiplied colour of a pixel. Transparent PNG
// pixels keep their raw channels, matching cv2.imread without IMREAD_UNCHANGED.
func rawRGB(img image.Image, x, y int) (uint8, uint8, uint8) {
	switch m := img.(type) {
	case *image.NRGBA:
		i := (y-m.Rect.Min.Y)*m.Stride + (x-m.Rect.Min.X)*4
		return m.Pix[i], m.Pix[i+1], m.Pix[i+2]
	case *image.RGBA:
		i := (y-m.Rect.Min.Y)*m.Stride + (x-m.Rect.Min.X)*4
		a := m.Pix[i+3]
		if a == 0 {
			return 0, 0, 0
		}
		if a == 255 {
			return m.Pix[i], m.Pix[i+1], m.Pix[i+2]
		}
		return uint8(int(m.Pix[i]) * 255 / int(a)),
			uint8(int(m.Pix[i+1]) * 255 / int(a)),
			uint8(int(m.Pix[i+2]) * 255 / int(a))
	case *image.Gray:
		v := m.Pix[(y-m.Rect.Min.Y)*m.Stride+(x-m.Rect.Min.X)]
		return v, v, v
	default:
		c := colorNRGBA(img, x, y)
		return c.R, c.G, c.B
	}
}

func colorNRGBA(img image.Image, x, y int) color.NRGBA {
	return color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
}

func luma(r, g, b uint8) uint8 {
	// Rec. 601 luma, close enough to the conversions OpenCV uses here.
	return uint8((int(r)*299 + int(g)*587 + int(b)*114) / 1000)
}

func grayStats(p []uint8) (float64, float64) {
	if len(p) == 0 {
		return 0, 0
	}
	n := float64(len(p))
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
