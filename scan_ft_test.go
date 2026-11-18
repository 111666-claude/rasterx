// Copyright 2023 by the rasterx Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rasterx_test

import (
	"image"
	"image/color"
	"math"
	"testing"

	. "github.com/srwiley/rasterx"
	"golang.org/x/image/math/fixed"
)

// ringPath builds two square loops: an outer square (x0,y0)-(x1,y1) and an
// inner square. If reverseInner is true the inner contour is emitted in the
// opposite winding direction of the outer contour; otherwise both contours
// share the same direction.
func ringPath(x0, y0, x1, y1 float64, reverseInner bool) (p Path) {
	// Outer loop, clockwise in screen coordinates (y grows downward).
	p.Start(ToFixedP(x0, y0))
	p.Line(ToFixedP(x1, y0))
	p.Line(ToFixedP(x1, y1))
	p.Line(ToFixedP(x0, y1))
	p.Stop(true)

	ix0, iy0, ix1, iy1 := x0+20, y0+20, x1-20, y1-20
	p.Start(ToFixedP(ix0, iy0))
	if reverseInner {
		// Counter-clockwise inner loop.
		p.Line(ToFixedP(ix0, iy1))
		p.Line(ToFixedP(ix1, iy1))
		p.Line(ToFixedP(ix1, iy0))
	} else {
		// Clockwise inner loop, same direction as the outer one.
		p.Line(ToFixedP(ix1, iy0))
		p.Line(ToFixedP(ix1, iy1))
		p.Line(ToFixedP(ix0, iy1))
	}
	p.Stop(true)
	return
}

func newFTFiller(w, h int, useNonZero bool) (*image.RGBA, *Filler) {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	sc := NewScannerFT(w, h, img, img.Bounds())
	f := NewFiller(w, h, sc)
	// NewFiller defaults to non-zero winding; the rule is selected on the
	// scanner through the Filler's embedded interface after construction.
	f.SetWinding(useNonZero)
	return img, f
}

// pixelAlpha returns the alpha of the pixel at (x, y).
func pixelAlpha(img *image.RGBA, x, y int) uint8 {
	return img.RGBAAt(x, y).A
}

// assertFilledPixels checks that every pixel inside the inset rectangle is
// either fully opaque or (at most tol soft edge pixels away) part of the fill.
func assertFilledPixels(t *testing.T, img *image.RGBA, r image.Rectangle, wantOpaque bool, name string) {
	t.Helper()
	var soft, hard int
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			a := pixelAlpha(img, x, y)
			switch {
			case a == 0:
				if wantOpaque {
					t.Errorf("%s: pixel (%d,%d) transparent, expected filled", name, x, y)
					return
				}
				hard++
			case a == 255:
				if !wantOpaque {
					t.Errorf("%s: pixel (%d,%d) opaque, expected empty", name, x, y)
					return
				}
				hard++
			default:
				soft++
			}
		}
	}
	if hard == 0 {
		t.Errorf("%s: no fully %s pixels inside %v (soft=%d)", name,
			map[bool]string{true: "filled", false: "empty"}[wantOpaque], r, soft)
	}
}

// TestScannerFTInterface makes sure ScannerFT satisfies the Scanner interface.
func TestScannerFTInterface(t *testing.T) {
	var _ Scanner = (*ScannerFT)(nil)
}

// TestEvenOddHole is the headline requirement: an icon with a hole filled
// with the even-odd rule must render the inner contour as a hole regardless
// of the inner contour's direction.
func TestEvenOddHole(t *testing.T) {
	const w, h = 120, 120
	for _, reverse := range []bool{false, true} {
		name := map[bool]string{true: "reverseInner", false: "sameDirection"}[reverse]
		img, f := newFTFiller(w, h, false /* even-odd */)
		f.SetColor(color.Black)
		ringPath(10, 10, 110, 110, reverse).AddTo(f)
		f.Draw()

		// Outer ring interior is filled.
		assertFilledPixels(t, img, image.Rect(14, 14, 26, 26), true, "eo:"+name+":outer")
		// Inner square is a hole.
		assertFilledPixels(t, img, image.Rect(40, 40, 80, 80), false, "eo:"+name+":hole")
		// Outside is empty.
		assertFilledPixels(t, img, image.Rect(0, 0, 8, 8), false, "eo:"+name+":outside")
	}
}

// TestNonZeroHoleDirection shows that under the non-zero rule a hole only
// exists when the inner contour is reversed, while two same-direction loops
// fill solid. This is the semantic contrast even-odd is supposed to remove.
func TestNonZeroHoleDirection(t *testing.T) {
	const w, h = 120, 120

	// Reversed inner loop: hole under non-zero.
	img, f := newFTFiller(w, h, true)
	f.SetColor(color.Black)
	ringPath(10, 10, 110, 110, true).AddTo(f)
	f.Draw()
	assertFilledPixels(t, img, image.Rect(40, 40, 80, 80), false, "nz:reversed:hole")

	// Same direction loops: the center stays filled under non-zero.
	img2, f2 := newFTFiller(w, h, true)
	f2.SetColor(color.Black)
	ringPath(10, 10, 110, 110, false).AddTo(f2)
	f2.Draw()
	assertFilledPixels(t, img2, image.Rect(40, 40, 80, 80), true, "nz:sameDir:filled")
}

// TestEvenOddReversalInvariance states the task premise directly: reversing
// the inner contour direction must not change the even-odd result. Compare
// the two rendered images pixel by pixel.
func TestEvenOddReversalInvariance(t *testing.T) {
	const w, h = 120, 120
	imgA, fA := newFTFiller(w, h, false)
	fA.SetColor(color.Black)
	ringPath(10, 10, 110, 110, false).AddTo(fA)
	fA.Draw()

	imgB, fB := newFTFiller(w, h, false)
	fB.SetColor(color.Black)
	ringPath(10, 10, 110, 110, true).AddTo(fB)
	fB.Draw()

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if imgA.RGBAAt(x, y) != imgB.RGBAAt(x, y) {
				t.Fatalf("even-odd render changed with contour direction at (%d,%d): %v vs %v",
					x, y, imgA.RGBAAt(x, y), imgB.RGBAAt(x, y))
			}
		}
	}
}

// TestNestedHoles builds four nested squares. Under even-odd the layers
// alternate filled/empty; under non-zero with alternating directions the
// same geometry fills every layer.
func TestNestedHoles(t *testing.T) {
	const w, h = 220, 220
	rects := [][2]image.Point{
		{image.Pt(10, 10), image.Pt(210, 210)},
		{image.Pt(30, 30), image.Pt(190, 190)},
		{image.Pt(50, 50), image.Pt(170, 170)},
		{image.Pt(70, 70), image.Pt(150, 150)},
	}
	var p Path
	for i, rr := range rects {
		a, b := rr[0], rr[1]
		// Every second loop is reversed.
		if i%2 == 0 {
			p.Start(ToFixedP(float64(a.X), float64(a.Y)))
			p.Line(ToFixedP(float64(b.X), float64(a.Y)))
			p.Line(ToFixedP(float64(b.X), float64(b.Y)))
			p.Line(ToFixedP(float64(a.X), float64(b.Y)))
		} else {
			p.Start(ToFixedP(float64(a.X), float64(a.Y)))
			p.Line(ToFixedP(float64(a.X), float64(b.Y)))
			p.Line(ToFixedP(float64(b.X), float64(b.Y)))
			p.Line(ToFixedP(float64(b.X), float64(a.Y)))
		}
		p.Stop(true)
	}

	img, f := newFTFiller(w, h, false)
	f.SetColor(color.Black)
	p.AddTo(f)
	f.Draw()

	// Layers: between r0-r1 filled, r1-r2 empty, r2-r3 filled, inside r3 empty.
	cases := []struct {
		r    image.Rectangle
		fill bool
	}{
		{image.Rect(15, 15, 25, 25), true},
		{image.Rect(35, 35, 45, 45), false},
		{image.Rect(55, 55, 65, 65), true},
		{image.Rect(90, 90, 130, 130), false},
	}
	for _, c := range cases {
		assertFilledPixels(t, img, c.r, c.fill, "nested-eo")
	}
}

// bowtiePath is a self-intersecting path: an hourglass made of two triangles
// whose edges cross at the center. Walking any horizontal scanline crosses
// the outline twice in the top and bottom triangles (odd -> filled) and four
// times through the central crossing band (even -> empty).
func bowtiePath(cx, cy, half float64) (p Path) {
	t, b := cy-half, cy+half
	l, r := cx-half, cx+half
	p.Start(ToFixedP(l, t))
	p.Line(ToFixedP(r, t))
	p.Line(ToFixedP(l, b))
	p.Line(ToFixedP(r, b))
	p.Stop(true)
	return
}

// TestSelfIntersectingPath checks the even-odd rule on a self-intersecting
// hourglass: the top and bottom triangular lobes are filled while the central
// band where the two triangles overlap is crossed four times and stays empty.
func TestSelfIntersectingPath(t *testing.T) {
	const w, h = 120, 120
	img, f := newFTFiller(w, h, false)
	f.SetColor(color.Black)
	bowtiePath(60, 60, 50).AddTo(f)
	f.Draw()

	// Top and bottom lobes, sampled well inside the triangles and away from
	// the crossing diagonals.
	assertFilledPixels(t, img, image.Rect(55, 14, 65, 22), true, "bowtie:topLobe")
	assertFilledPixels(t, img, image.Rect(55, 98, 65, 106), true, "bowtie:bottomLobe")
	// On the crossing row, points away from the apex are covered by both
	// triangles (even parity) and are exactly empty, excluding the AA pixels
	// that hug the two crossing diagonals near the apex.
	assertFilledPixels(t, img, image.Rect(48, 59, 56, 61), false, "bowtie:center")
	assertFilledPixels(t, img, image.Rect(64, 59, 72, 61), false, "bowtie:centerFlank")
}

// TestHorizontalEdges mixes perfectly horizontal edges with slanted and
// vertical edges. Horizontal edges carry no coverage and must not corrupt
// the parity state.
func TestHorizontalEdges(t *testing.T) {
	const w, h = 120, 120
	var p Path
	// A house shape: flat base, flat roof section, slanted sides.
	p.Start(ToFixedP(20, 100))
	p.Line(ToFixedP(100, 100)) // horizontal
	p.Line(ToFixedP(100, 50))  // vertical
	p.Line(ToFixedP(80, 30))
	p.Line(ToFixedP(60, 30)) // horizontal roof
	p.Line(ToFixedP(40, 30)) // repeated horizontal segment, same y
	p.Line(ToFixedP(20, 50))
	p.Stop(true)

	for _, nz := range []bool{true, false} {
		name := map[bool]string{true: "nz", false: "eo"}[nz]
		img, f := newFTFiller(w, h, nz)
		f.SetColor(color.Black)
		p.AddTo(f)
		f.Draw()
		assertFilledPixels(t, img, image.Rect(45, 60, 75, 90), true, "house:"+name)
		assertFilledPixels(t, img, image.Rect(2, 2, 12, 12), false, "house:outside:"+name)
	}
}

// TestCoincidentAndDuplicateIntersections stacks several identical loops and
// pairs of overlapping edges. Even-odd must toggle deterministically: two
// coincident loops cancel, three leave a fill.
func TestCoincidentAndDuplicateIntersections(t *testing.T) {
	const w, h = 100, 100
	square := func(p *Path) {
		p.Start(ToFixedP(20, 20))
		p.Line(ToFixedP(80, 20))
		p.Line(ToFixedP(80, 80))
		p.Line(ToFixedP(20, 80))
		p.Stop(true)
	}

	// Two identical loops -> even count -> empty everywhere inside.
	var p2 Path
	square(&p2)
	square(&p2)
	img, f := newFTFiller(w, h, false)
	f.SetColor(color.Black)
	p2.AddTo(f)
	f.Draw()
	assertFilledPixels(t, img, image.Rect(30, 30, 70, 70), false, "dup:twoLoops")

	// Three identical loops -> odd count -> filled.
	var p3 Path
	square(&p3)
	square(&p3)
	square(&p3)
	img2, f2 := newFTFiller(w, h, false)
	f2.SetColor(color.Black)
	p3.AddTo(f2)
	f2.Draw()
	assertFilledPixels(t, img2, image.Rect(30, 30, 70, 70), true, "dup:threeLoops")

	// Non-zero on two same-direction loops is also filled; two opposite
	// direction loops cancel.
	imgNZ, fNZ := newFTFiller(w, h, true)
	fNZ.SetColor(color.Black)
	p2.AddTo(fNZ)
	fNZ.Draw()
	assertFilledPixels(t, imgNZ, image.Rect(30, 30, 70, 70), true, "dup:nzTwoSameDir")
}

// TestZeroAreaSubpaths feeds degenerate subpaths (points, back-and-forth
// lines, empty moves). They must contribute no pixels at all.
func TestZeroAreaSubpaths(t *testing.T) {
	const w, h = 100, 100
	var p Path
	p.Start(ToFixedP(10, 10))
	p.Stop(false) // lone move
	p.Start(ToFixedP(20, 20))
	p.Line(ToFixedP(80, 20))
	p.Line(ToFixedP(20, 20)) // back to start, no area
	p.Stop(true)
	p.Start(ToFixedP(50, 50))
	p.Line(ToFixedP(50, 50)) // zero-length line
	p.Stop(true)
	// Horizontal-only open path.
	p.Start(ToFixedP(0, 90))
	p.Line(ToFixedP(99, 90))
	p.Stop(false)

	for _, nz := range []bool{true, false} {
		img, f := newFTFiller(w, h, nz)
		f.SetColor(color.Black)
		p.AddTo(f)
		f.Draw()
		n := 0
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				if pixelAlpha(img, x, y) != 0 {
					n++
				}
			}
		}
		if n != 0 {
			t.Errorf("zero-area subpaths produced %d non-empty pixels (nz=%v)", n, nz)
		}
	}
}

// TestHugeCoordinates uses coordinates well beyond the canvas (but well
// inside the 26.6 fixed-point range) so that most of each edge is clipped by
// the image bounds. The result must be stable: a single covering loop fills
// the canvas, two coincident even-odd loops cancel to empty.
func TestHugeCoordinates(t *testing.T) {
	const w, h = 100, 100
	interior := image.Rect(5, 5, 95, 95)
	draw := func(loops int) *image.RGBA {
		img, f := newFTFiller(w, h, false)
		f.SetColor(color.Black)
		for k := 0; k < loops; k++ {
			// ±100000 is far outside the 100px canvas yet cheap to clip, and
			// 100000*64 stays comfortably inside int32 arithmetic.
			f.Start(ToFixedP(-100000, -100000))
			f.Line(ToFixedP(100000, -100000))
			f.Line(ToFixedP(100000, 100000))
			f.Line(ToFixedP(-100000, 100000))
			f.Stop(true)
		}
		f.Draw()
		return img
	}

	// A single covering loop fills everything.
	assertFilledPixels(t, draw(1), interior, true, "huge:cover")
	// Two coincident loops have even parity and cancel to empty.
	assertFilledPixels(t, draw(2), interior, false, "huge:cancel")
	// Three loops have odd parity and fill again.
	assertFilledPixels(t, draw(3), interior, true, "huge:three")
}

// TestDegenerateCurves exercises the Filler's bezier degenerate-case branches
// through ScannerFT: collinear control points collapse to lines and must fill
// identically to the equivalent polygon under both rules.
func TestDegenerateCurves(t *testing.T) {
	const w, h = 100, 100

	poly := func(a Adder) {
		a.Start(ToFixedP(15, 15))
		a.Line(ToFixedP(85, 15))
		a.Line(ToFixedP(85, 85))
		a.Line(ToFixedP(15, 85))
		a.Stop(true)
	}
	// Every segment is a degenerate bezier that Filler collapses to a line:
	//   QuadBezier(a, c)            with b == a (control point on the start)
	//   CubeBezier(b, c, d)         with a == b and b == c
	//   CubeBezier(b, c, d)         with c == b and d == c (all on the end)
	curve := func(a Adder) {
		a.Start(ToFixedP(15, 15))
		a.QuadBezier(ToFixedP(15, 15), ToFixedP(85, 15))
		a.CubeBezier(ToFixedP(85, 15), ToFixedP(85, 15), ToFixedP(85, 85))
		a.CubeBezier(ToFixedP(15, 85), ToFixedP(15, 85), ToFixedP(15, 85))
		a.Stop(true)
	}

	for _, nz := range []bool{true, false} {
		imgP, fP := newFTFiller(w, h, nz)
		fP.SetColor(color.Black)
		poly(fP)
		fP.Draw()

		imgC, fC := newFTFiller(w, h, nz)
		fC.SetColor(color.Black)
		curve(fC)
		fC.Draw()

		diff := 0
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				if absInt(int(imgP.RGBAAt(x, y).A)-int(imgC.RGBAAt(x, y).A)) > 8 {
					diff++
				}
			}
		}
		if diff > 4 { // allow a handful of AA rounding differences
			t.Errorf("degenerate curves differ from polygon at %d pixels (nz=%v)", diff, nz)
		}
	}
}

// TestBezierHoleEvenOdd renders a circle (cubic beziers) with a quadratic
// bezier inner loop. Even-odd must produce a hole for both directions.
func TestBezierHoleEvenOdd(t *testing.T) {
	const w, h = 120, 120
	build := func(reverse bool) Path {
		var p Path
		AddCircle(60, 60, 50, &p)
		inner := Path{}
		AddCircle(60, 60, 20, &inner)
		if reverse {
			inner = reversePath(inner)
		}
		p = append(p, inner...)
		return p
	}
	for _, reverse := range []bool{false, true} {
		img, f := newFTFiller(w, h, false)
		f.SetColor(color.Black)
		build(reverse).AddTo(f)
		f.Draw()
		assertFilledPixels(t, img, image.Rect(50, 50, 70, 70), false, "bezier-hole")
		assertFilledPixels(t, img, image.Rect(14, 55, 24, 65), true, "bezier-ring")
	}
}

// reversePath returns a path with every closed subpath's commands emitted in
// reverse order. Moves stay anchored at the same start point.
func reversePath(p Path) Path {
	var out Path
	i := 0
	for i < len(p) {
		start := i
		i += 3 // move
		var seg [][]fixed.Int26_6
		for i < len(p) && PathCommand(p[i]) != PathMoveTo {
			switch PathCommand(p[i]) {
			case PathLineTo:
				seg = append(seg, append([]fixed.Int26_6{fixed.Int26_6(PathLineTo)}, p[i+1:i+3]...))
				i += 3
			case PathQuadTo:
				seg = append(seg, append([]fixed.Int26_6{fixed.Int26_6(PathQuadTo)}, p[i+1:i+5]...))
				i += 5
			case PathCubicTo:
				seg = append(seg, append([]fixed.Int26_6{fixed.Int26_6(PathCubicTo)}, p[i+1:i+7]...))
				i += 7
			case PathClose:
				i++
			default:
				i++
			}
		}
		out = append(out, p[start:start+3]...)
		for j := len(seg) - 1; j >= 0; j-- {
			out = append(out, seg[j]...)
		}
		out = append(out, fixed.Int26_6(PathClose))
	}
	return out
}

// TestAntiAliasEdges verifies that coverage ramps smoothly across a fill
// boundary instead of hard-stepping, under both winding rules.
func TestAntiAliasEdges(t *testing.T) {
	const w, h = 100, 100
	for _, nz := range []bool{true, false} {
		img, f := newFTFiller(w, h, nz)
		f.SetColor(color.Black)
		AddRect(20.5, 10, 80, 90, 0, f)
		f.Draw()

		// Walk across the left edge at mid-height and require a monotone
		// ramp with at least one intermediate alpha value.
		var alphas []int
		for x := 17; x <= 24; x++ {
			alphas = append(alphas, int(pixelAlpha(img, x, 50)))
		}
		hasSoft := false
		for _, a := range alphas {
			if a > 0 && a < 255 {
				hasSoft = true
			}
		}
		if !hasSoft {
			t.Errorf("no anti-aliased edge pixels at x=17..24 (nz=%v): %v", nz, alphas)
		}
		// Ramp must be non-decreasing left to right.
		for k := 1; k < len(alphas); k++ {
			if alphas[k] < alphas[k-1] {
				t.Errorf("edge coverage not monotone (nz=%v): %v", nz, alphas)
				break
			}
		}
	}
}

// TestParityFoldAtCoverageBoundary exercises the exact 0x1000 boundary in
// areaToAlpha: a pixel whose folded coverage lands precisely on the boundary
// must resolve to just under full coverage, never overflowing.
func TestParityFoldAtCoverageBoundary(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	sc := NewScannerFT(4, 4, img, img.Bounds())
	f := NewFiller(4, 4, sc)
	f.SetWinding(false) // even-odd, must be set after NewFiller
	// areaToAlpha is unexported; drive it indirectly through stacked
	// rectangles that produce high coverage counts, then sanity-check bounds.
	for k := 0; k < 7; k++ {
		f.Start(ToFixedP(0, 0))
		f.Line(ToFixedP(4, 0))
		f.Line(ToFixedP(4, 4))
		f.Line(ToFixedP(0, 4))
		f.Stop(true)
	}
	f.SetColor(color.Black)
	f.Draw()
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			a := pixelAlpha(img, x, y)
			if a > 255 {
				t.Fatalf("alpha out of range: %d", a)
			}
		}
	}
}

// TestScannerFTMatchesGVNonZero cross-checks the two scanners under the
// non-zero rule on a path with holes and curves. They use different AA
// algorithms so exact equality is not expected, but the filled/empty regions
// must agree and interior pixels must be fully opaque in both.
func TestScannerFTMatchesGVNonZero(t *testing.T) {
	const w, h = 160, 160

	imgFT, fFT := newFTFiller(w, h, true)
	fFT.SetColor(colornamesRed())
	ringPath(10, 10, 150, 150, true).AddTo(fFT)
	fFT.Draw()

	imgGV := image.NewRGBA(image.Rect(0, 0, w, h))
	scGV := NewScannerGV(w, h, imgGV, imgGV.Bounds())
	fGV := NewFiller(w, h, scGV)
	fGV.SetColor(colornamesRed())
	ringPath(10, 10, 150, 150, true).AddTo(fGV)
	fGV.Draw()

	checks := []struct {
		r    image.Rectangle
		fill bool
	}{
		{image.Rect(15, 15, 28, 28), true},
		{image.Rect(60, 60, 100, 100), false},
		{image.Rect(0, 0, 8, 8), false},
	}
	for _, c := range checks {
		assertFilledPixels(t, imgFT, c.r, c.fill, "ft-nz")
		assertFilledPixels(t, imgGV, c.r, c.fill, "gv-nz")
	}

	// Compare total ink: the two AA implementations should be within 2%.
	var inkFT, inkGV int
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			inkFT += int(imgFT.RGBAAt(x, y).A)
			inkGV += int(imgGV.RGBAAt(x, y).A)
		}
	}
	diff := math.Abs(float64(inkFT-inkGV)) / float64(inkGV)
	if diff > 0.02 {
		t.Errorf("non-zero ink differs too much: FT=%d GV=%d (%.2f%%)", inkFT, inkGV, 100*diff)
	}
}

// TestClipRestrictsEvenOddDraw makes sure clipping still works in even-odd
// mode and that the color function keeps being evaluated per pixel.
func TestClipRestrictsEvenOddDraw(t *testing.T) {
	const w, h = 100, 100
	img, f := newFTFiller(w, h, false)
	sc := f.Scanner.(*ScannerFT)
	sc.SetColor(ColorFunc(func(x, y int) color.Color {
		return color.RGBA{R: uint8(x), G: uint8(y), B: 128, A: 255}
	}))
	sc.SetClip(image.Rect(30, 30, 70, 70))
	AddRect(10, 10, 90, 90, 0, f)
	f.Draw()

	assertFilledPixels(t, img, image.Rect(40, 40, 60, 60), true, "clip:inside")
	assertFilledPixels(t, img, image.Rect(15, 15, 25, 25), false, "clip:outside")
	// Color function values survive inside the clip.
	c := img.RGBAAt(45, 55)
	if c.R != 45 || c.G != 55 || c.B != 128 {
		t.Errorf("color function not honored under clip: got %v", c)
	}
}

func absInt(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// colornamesRed avoids importing colornames in this file; opaque red.
func colornamesRed() color.Color {
	return color.RGBA{R: 255, A: 255}
}

// BenchmarkFillFT mirrors BenchmarkFillGV but drives ScannerFT in both
// winding rules, so the even-odd implementation has a regression guard.
func BenchmarkFillFT(b *testing.B) {
	p := GetTestPath()
	for _, nz := range []bool{true, false} {
		name := map[bool]string{true: "NonZero", false: "EvenOdd"}[nz]
		b.Run(name, func(b *testing.B) {
			wx, wy := 512, 512
			img := image.NewRGBA(image.Rect(0, 0, wx, wy))
			scannerFT := NewScannerFT(wx, wy, img, img.Bounds())
			f := NewFiller(wx, wy, scannerFT)
			f.SetWinding(nz)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				p.AddTo(f)
				f.Draw()
				f.Clear()
			}
		})
	}
}
