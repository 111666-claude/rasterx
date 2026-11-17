// Copyright 2026 by the rasterx Authors. All rights reserved.
//
// Tests for the non-scaling stroke mode (vector-effect="non-scaling-stroke"):
// the affine transform is applied to the path geometry and the stroke is
// then built in device space with a constant on-screen width.

package rasterx_test

import (
	"image"
	"math"
	"testing"

	. "github.com/srwiley/rasterx"
	"golang.org/x/image/colornames"
	"golang.org/x/image/math/fixed"
)

// strokeStyle collects the parameters shared by the test renderers.
type strokeStyle struct {
	width      fixed.Int26_6
	miterLimit fixed.Int26_6
	capL, capT CapFunc
	gap        GapFunc
	join       JoinMode
	dashes     []float64
	dashOffset float64
}

func defaultStyle() strokeStyle {
	return strokeStyle{
		width:      6 * 64,
		miterLimit: 4 * 64,
		capL:       RoundCap,
		capT:       RoundCap,
		gap:        RoundGap,
		join:       ArcClip,
	}
}

// renderNS renders p through a non-scaling Stroker/Dasher using matrix m.
// The stroke width, caps, joins and dashes are interpreted in device space.
func renderNS(size int, p Path, st strokeStyle, m Matrix2D) *image.RGBA {
	img, adder, draw := nsAdder(size, st, m)
	p.AddTo(adder)
	draw()
	return img
}

// nsAdder returns the device-space non-scaling adder, its image and a draw
// callback. The callback must be invoked after the path has been added.
func nsAdder(size int, st strokeStyle, m Matrix2D) (*image.RGBA, Adder, func()) {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	sc := NewScannerGV(size, size, img, img.Bounds())
	sc.SetColor(colornames.Red)
	if len(st.dashes) > 0 {
		d := NewDasher(size, size, sc)
		d.SetNonScaling(true, m)
		d.SetStroke(st.width, st.miterLimit, st.capL, st.capT, st.gap, st.join,
			st.dashes, st.dashOffset)
		return img, d, d.Draw
	}
	s := NewStroker(size, size, sc)
	s.SetNonScaling(true, m)
	s.SetStroke(st.width, st.miterLimit, st.capL, st.capT, st.gap, st.join)
	return img, s, s.Draw
}

// renderRef renders the same geometry as renderNS but with every path point
// pre-transformed by m and a normal (non-scaling) stroker. This is the
// reference definition of "transform the geometry, then stroke in device
// space"; the two images must be identical.
func renderRef(size int, p Path, st strokeStyle, m Matrix2D) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	sc := NewScannerGV(size, size, img, img.Bounds())
	sc.SetColor(colornames.Red)
	if len(st.dashes) > 0 {
		d := NewDasher(size, size, sc)
		d.SetStroke(st.width, st.miterLimit, st.capL, st.capT, st.gap, st.join,
			st.dashes, st.dashOffset)
		ma := &MatrixAdder{Adder: d, M: m}
		p.AddTo(ma)
		d.Draw()
	} else {
		s := NewStroker(size, size, sc)
		s.SetStroke(st.width, st.miterLimit, st.capL, st.capT, st.gap, st.join)
		ma := &MatrixAdder{Adder: s, M: m}
		p.AddTo(ma)
		s.Draw()
	}
	return img
}

// alphaAt returns the alpha of pixel (x,y).
func alphaAt(img *image.RGBA, x, y int) uint8 {
	return img.RGBAAt(x, y).A
}

// painted reports whether pixel (x,y) has any coverage.
func painted(img *image.RGBA, x, y int) bool {
	return alphaAt(img, x, y) > 0
}

// imagesEqual reports pixel-for-pixel equality.
func imagesEqual(a, b *image.RGBA) bool {
	if a.Bounds() != b.Bounds() {
		return false
	}
	for i := range a.Pix {
		if a.Pix[i] != b.Pix[i] {
			return false
		}
	}
	return true
}

// firstDiff returns the first coordinate where the two images differ.
func firstDiff(a, b *image.RGBA) (x, y int) {
	for y = a.Bounds().Min.Y; y < a.Bounds().Max.Y; y++ {
		for x = a.Bounds().Min.X; x < a.Bounds().Max.X; x++ {
			if a.RGBAAt(x, y) != b.RGBAAt(x, y) {
				return x, y
			}
		}
	}
	return -1, -1
}

// mkMixedPath builds a path containing a horizontal line, a joined polyline
// and open cubic beziers, all in user space.
func mkMixedPath() Path {
	var p Path
	p.Start(ToFixedP(20, 40))
	p.Line(ToFixedP(110, 40)) // horizontal line
	p.Stop(false)

	p.Start(ToFixedP(30, 120))
	p.Line(ToFixedP(90, 90)) // joined polyline
	p.Line(ToFixedP(130, 130))
	p.Stop(false)

	p.Start(ToFixedP(20, 180))
	p.CubeBezier(ToFixedP(60, 150), ToFixedP(100, 220), ToFixedP(140, 190))
	p.CubeBezier(ToFixedP(150, 170), ToFixedP(90, 240), ToFixedP(60, 210))
	p.Stop(false)
	return p
}

// TestNonScalingMatchesDeviceSpaceReference verifies the core contract:
// non-scaling stroking must be exactly the same as transforming the path
// geometry first and stroking the result with an ordinary stroker. It covers
// horizontal lines and cubic beziers, with the Stroker and the Dasher, under
// non-uniform scaling, rotation and shear.
func TestNonScalingMatchesDeviceSpaceReference(t *testing.T) {
	const size = 300

	matrices := map[string]Matrix2D{
		"uniform":     Identity.Translate(20, 10).Scale(1.5, 1.5),
		"non-uniform": Identity.Translate(15, 25).Scale(2.0, 0.75),
		"rotation":    Identity.Translate(150, 150).Rotate(math.Pi/7).Translate(-70, -120),
		"shear":       Identity.Translate(10, 10).SkewX(0.35).SkewY(0.15).Scale(1.2, 1.1),
	}

	styles := map[string]strokeStyle{
		"solid": defaultStyle(),
		"dash":  func() strokeStyle { s := defaultStyle(); s.dashes = []float64{14, 8}; s.dashOffset = 3; return s }(),
	}

	for mName, m := range matrices {
		for sName, st := range styles {
			got := renderNS(size, mkMixedPath(), st, m)
			want := renderRef(size, mkMixedPath(), st, m)
			if !imagesEqual(got, want) {
				x, y := firstDiff(got, want)
				t.Errorf("%s/%s: non-scaling render differs from device-space reference at (%d,%d) got=%v want=%v",
					mName, sName, x, y, got.RGBAAt(x, y), want.RGBAAt(x, y))
			}
		}
	}
}

// TestNonScalingConstantPixelWidth checks the boundary pixels of a horizontal
// line rendered under two different uniform zoom factors. The stroke must
// occupy the same number of device rows with the same coverage profile, and
// the painted band must remain exactly width px thick even though the
// underlying geometry is scaled.
func TestNonScalingConstantPixelWidth(t *testing.T) {
	const (
		size     = 200
		deviceY  = 60 // device-space center line
		strokePx = 6  // requested device-space stroke width
	)

	render := func(scale float64) *image.RGBA {
		// Half-pixel offsets keep the transformed center line on the same
		// device pixel row for both scales, so the profiles are directly
		// comparable.
		ty := float64(deviceY) - 0.5*scale
		m := Identity.Translate(40, ty).Scale(scale, scale)
		var p Path
		p.Start(ToFixedP(10, 0))
		p.Line(ToFixedP(60, 0)) // user-space horizontal line
		p.Stop(false)
		st := defaultStyle()
		st.width = strokePx * 64
		st.capL, st.capT = ButtCap, ButtCap
		return renderNS(size, p, st, m)
	}

	img2, img4 := render(2), render(4)

	// Row profiles: the set of painted rows and the per-pixel alpha on each
	// row. A horizontal, fully-covered stroke must be fully opaque (alpha 255)
	// on every painted pixel and occupy exactly strokePx rows regardless of
	// the zoom; the two renders differ only in where those rows sit and in
	// how many columns they span.
	rowProfile := func(img *image.RGBA) (rows []int, allOpaque bool) {
		allOpaque = true
		for y := 0; y < size; y++ {
			rowHit := false
			for x := 0; x < size; x++ {
				if a := alphaAt(img, x, y); a > 0 {
					rowHit = true
					if a != 255 {
						allOpaque = false
					}
				}
			}
			if rowHit {
				rows = append(rows, y)
			}
		}
		return rows, allOpaque
	}

	rows2, opaque2 := rowProfile(img2)
	rows4, opaque4 := rowProfile(img4)
	if len(rows2) != strokePx || len(rows4) != strokePx {
		t.Errorf("stroke thickness changed with zoom: scale2 rows=%d scale4 rows=%d, want %d each",
			len(rows2), len(rows4), strokePx)
	}
	if !opaque2 || !opaque4 {
		t.Errorf("interior stroke pixels not fully opaque: scale2=%v scale4=%v", opaque2, opaque4)
	}
	// Rows must be contiguous (a solid band).
	contiguous := func(rows []int) bool {
		for i := 1; i < len(rows); i++ {
			if rows[i] != rows[i-1]+1 {
				return false
			}
		}
		return true
	}
	if !contiguous(rows2) || !contiguous(rows4) {
		t.Errorf("stroke band not contiguous: rows2=%v rows4=%v", rows2, rows4)
	}

	// The geometry still follows the transform: scale 4 spans more device
	// columns than scale 2.
	span := func(img *image.RGBA) (min, max int) {
		min, max = size, -1
		for x := 0; x < size; x++ {
			for y := 0; y < size; y++ {
				if painted(img, x, y) {
					if x < min {
						min = x
					}
					if x > max {
						max = x
					}
				}
			}
		}
		return min, max
	}
	min2, max2 := span(img2)
	min4, max4 := span(img4)
	if max4-min4 <= max2-min2 {
		t.Errorf("geometry did not scale: span2=%d span4=%d", max2-min2, max4-min4)
	}
}

// columnProfile reports which columns contain any painted pixel.
func columnProfile(img *image.RGBA, size int) []bool {
	on := make([]bool, size)
	for x := 0; x < size; x++ {
		for y := 0; y < size; y++ {
			if painted(img, x, y) {
				on[x] = true
				break
			}
		}
	}
	return on
}

// dashRuns returns the [start,end] column runs that are painted, one per dash.
func dashRuns(on []bool) [][2]int {
	var runs [][2]int
	start := -1
	for x, v := range on {
		if v && start < 0 {
			start = x
		} else if !v && start >= 0 {
			runs = append(runs, [2]int{start, x - 1})
			start = -1
		}
	}
	if start >= 0 {
		runs = append(runs, [2]int{start, len(on) - 1})
	}
	return runs
}

// TestNonScalingDashLengthInDeviceSpace checks that dash and gap lengths are
// measured in device space: changing the zoom must not change the on-screen
// length of a dash.
func TestNonScalingDashLengthInDeviceSpace(t *testing.T) {
	const size = 240

	render := func(scale float64) *image.RGBA {
		m := Identity.Translate(20, 100).Scale(scale, scale)
		var p Path
		p.Start(ToFixedP(0, 0))
		p.Line(ToFixedP(90, 0))
		p.Stop(false)
		st := defaultStyle()
		st.capL, st.capT = ButtCap, ButtCap
		st.dashes = []float64{10, 10} // 10 device px dash, 10 device px gap
		return renderNS(size, p, st, m)
	}

	runs2 := dashRuns(columnProfile(render(2), size))
	runs3 := dashRuns(columnProfile(render(3), size))
	if len(runs2) == 0 || len(runs3) == 0 {
		t.Fatalf("no dashes rendered: runs2=%d runs3=%d", len(runs2), len(runs3))
	}
	for name, runs := range map[string][][2]int{"scale2": runs2, "scale3": runs3} {
		for i, rn := range runs {
			if l := rn[1] - rn[0] + 1; l < 9 || l > 11 {
				t.Errorf("%s dash %d length %d px, want ~10 device px", name, i, l)
			}
		}
	}
}

// TestNonScalingEdgeCases gives deterministic behavior for the degenerate
// inputs required by the specification: zero and negative width, singular
// matrices, empty paths and extreme coordinates.
func TestNonScalingEdgeCases(t *testing.T) {
	const size = 128

	mkLine := func() Path {
		var p Path
		p.Start(ToFixedP(10, 10))
		p.Line(ToFixedP(80, 80))
		p.Stop(false)
		return p
	}

	countPainted := func(img *image.RGBA) int {
		n := 0
		for y := 0; y < size; y++ {
			for x := 0; x < size; x++ {
				if painted(img, x, y) {
					n++
				}
			}
		}
		return n
	}

	// Zero and negative width must render nothing and must not panic.
	for _, w := range []fixed.Int26_6{0, -5 * 64} {
		st := defaultStyle()
		st.width = w
		if n := countPainted(renderNS(size, mkLine(), st, Identity.Scale(3, 3))); n != 0 {
			t.Errorf("width %d: rendered %d painted pixels, want 0", w, n)
		}
	}

	// Singular matrices (all-zero and rank-1) must not panic and must produce
	// deterministic output. With butt caps the all-zero matrix collapses every
	// point onto the origin, so the stroked segment degenerates to a point and
	// renders nothing; the rank-1 matrix collapses the direction but keeps the
	// two points distinct.
	buttStyle := defaultStyle()
	buttStyle.capL, buttStyle.capT = ButtCap, ButtCap
	singular := []Matrix2D{
		{},
		{A: 1, B: 1, C: 1, D: 1}, // rank-1: output lies on y == x
	}
	for i, m := range singular {
		func() {
			defer func() {
				if rec := recover(); rec != nil {
					t.Errorf("singular matrix %d panicked: %v", i, rec)
				}
			}()
			img := renderNS(size, mkLine(), buttStyle, m)
			if i == 0 && countPainted(img) != 0 {
				t.Errorf("zero matrix rendered %d painted pixels, want 0", countPainted(img))
			}
		}()
	}

	// Empty path: nothing rendered, no panic, for both stroker and dasher.
	if countPainted(renderNS(size, Path{}, defaultStyle(), Identity.Scale(2, 2))) != 0 {
		t.Error("empty solid path rendered pixels, want 0")
	}
	stDash := defaultStyle()
	stDash.dashes = []float64{5, 5}
	if countPainted(renderNS(size, Path{}, stDash, Identity.Scale(2, 2))) != 0 {
		t.Error("empty dashed path rendered pixels, want 0")
	}

	// Extreme coordinates under a scaling matrix: values that overflow the
	// fixed-point range must saturate instead of wrapping or panicking.
	func() {
		defer func() {
			if rec := recover(); rec != nil {
				t.Errorf("extreme coordinates panicked: %v", rec)
			}
		}()

		// Case 1: endpoints at the int32 limit. After the 2x transform they
		// are clamped to the float32-safe magnitude (far outside the canvas),
		// so this must not panic and must produce deterministic output.
		var p Path
		p.Start(fixed.Point26_6{X: fixed.Int26_6(math.MaxInt32), Y: 40 * 64})
		p.Line(fixed.Point26_6{X: fixed.Int26_6(math.MinInt32), Y: 40 * 64})
		p.Stop(false)
		imgA := renderNS(size, p, buttStyle, Identity.Scale(2, 2))
		imgB := renderNS(size, p, buttStyle, Identity.Scale(2, 2))
		if !imagesEqual(imgA, imgB) {
			t.Error("extreme-coordinate rendering is not deterministic across runs")
		}

		// Case 2: a very large but representable line whose band still crosses
		// the visible canvas. Its endpoints overflow a naive int32 transform,
		// yet saturation must keep the portion inside the bounds renderable.
		var q Path
		q.Start(fixed.Point26_6{X: -(1 << 26), Y: 20 * 64})
		q.Line(fixed.Point26_6{X: 1 << 26, Y: 20 * 64})
		q.Stop(false)
		img := renderNS(size, q, buttStyle, Identity)
		hit := false
		for x := 0; x < size; x++ {
			if painted(img, x, 20) {
				hit = true
				break
			}
		}
		if !hit {
			t.Error("large representable line produced no coverage on its visible row")
		}
	}()
}

// TestNonScalingDisabledIsUnchanged proves that in normal mode the
// Transformer is ignored and behavior is identical to the plain stroker.
func TestNonScalingDisabledIsUnchanged(t *testing.T) {
	const size = 128
	st := defaultStyle()
	var p Path
	p.Start(ToFixedP(20, 20))
	p.Line(ToFixedP(90, 60))
	p.CubeBezier(ToFixedP(40, 90), ToFixedP(100, 100), ToFixedP(70, 40))
	p.Stop(false)

	// NonScaling defaults to false even with a non-identity Transformer.
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	sc := NewScannerGV(size, size, img, img.Bounds())
	s := NewStroker(size, size, sc)
	s.SetNonScaling(false, Identity.Scale(7, 7).Rotate(0.9))
	s.SetStroke(st.width, st.miterLimit, st.capL, st.capT, st.gap, st.join)
	sc.SetColor(colornames.Red)
	p.AddTo(s)
	s.Draw()

	want := image.NewRGBA(image.Rect(0, 0, size, size))
	sc2 := NewScannerGV(size, size, want, want.Bounds())
	s2 := NewStroker(size, size, sc2)
	s2.SetStroke(st.width, st.miterLimit, st.capL, st.capT, st.gap, st.join)
	sc2.SetColor(colornames.Red)
	p.AddTo(s2)
	s2.Draw()

	if !imagesEqual(img, want) {
		x, y := firstDiff(img, want)
		t.Errorf("normal mode was affected by Transformer: diff at (%d,%d)", x, y)
	}
}

// TestNonScalingRoundTripIdentity ensures identity transformation in
// non-scaling mode matches an ordinary stroke exactly.
func TestNonScalingRoundTripIdentity(t *testing.T) {
	const size = 160
	st := defaultStyle()
	var p Path
	p.Start(ToFixedP(20, 30))
	p.Line(ToFixedP(120, 30))
	p.CubeBezier(ToFixedP(130, 80), ToFixedP(40, 110), ToFixedP(100, 130))
	p.Stop(true)

	got := renderNS(size, p, st, Identity)
	want := renderRef(size, p, st, Identity)
	if !imagesEqual(got, want) {
		x, y := firstDiff(got, want)
		t.Errorf("identity non-scaling differs from normal stroke at (%d,%d)", x, y)
	}
}
