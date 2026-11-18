// Copyright 2026 by the rasterx Authors. All rights reserved.
// Tests for path clipping (ScannerGV.SetClipPath).
package rasterx_test

import (
	"image"
	"image/color"
	"testing"

	. "github.com/srwiley/rasterx"
)

// circlePath returns a circle approximated by four cubic bezier segments.
// If reverse is true the contour is wound in the opposite direction.
func circlePath(cx, cy, r float64, reverse bool) (p Path) {
	const kappa = 0.5522847498307936 // circle to cubic bezier constant
	k := kappa * r
	if !reverse {
		p.Start(ToFixedP(cx+r, cy))
		p.CubeBezier(ToFixedP(cx+r, cy+k), ToFixedP(cx+k, cy+r), ToFixedP(cx, cy+r))
		p.CubeBezier(ToFixedP(cx-k, cy+r), ToFixedP(cx-r, cy+k), ToFixedP(cx-r, cy))
		p.CubeBezier(ToFixedP(cx-r, cy-k), ToFixedP(cx-k, cy-r), ToFixedP(cx, cy-r))
		p.CubeBezier(ToFixedP(cx+k, cy-r), ToFixedP(cx+r, cy-k), ToFixedP(cx+r, cy))
	} else {
		p.Start(ToFixedP(cx+r, cy))
		p.CubeBezier(ToFixedP(cx+r, cy-k), ToFixedP(cx+k, cy-r), ToFixedP(cx, cy-r))
		p.CubeBezier(ToFixedP(cx-k, cy-r), ToFixedP(cx-r, cy-k), ToFixedP(cx-r, cy))
		p.CubeBezier(ToFixedP(cx-r, cy+k), ToFixedP(cx-k, cy+r), ToFixedP(cx, cy+r))
		p.CubeBezier(ToFixedP(cx+k, cy+r), ToFixedP(cx+r, cy+k), ToFixedP(cx+r, cy))
	}
	p.Stop(true)
	return
}

// fillCanvas draws an opaque rectangle covering the whole canvas through f.
func fillCanvas(f *Filler, w, h float64) {
	var p Path
	p.Start(ToFixedP(0, 0))
	p.Line(ToFixedP(w, 0))
	p.Line(ToFixedP(w, h))
	p.Line(ToFixedP(0, h))
	p.Stop(true)
	p.AddTo(f)
	f.Draw()
	f.Clear()
}

func alphaOf(img *image.RGBA, x, y int) uint8 {
	_, _, _, a := img.At(x, y).RGBA()
	return uint8(a >> 8)
}

func samePixels(a, b *image.RGBA) bool {
	if !a.Bounds().Eq(b.Bounds()) {
		return false
	}
	for y := a.Bounds().Min.Y; y < a.Bounds().Max.Y; y++ {
		for x := a.Bounds().Min.X; x < a.Bounds().Max.X; x++ {
			ra, ga, ba, aa := a.At(x, y).RGBA()
			rb, gb, bb, ab := b.At(x, y).RGBA()
			if ra != rb || ga != gb || ba != bb || aa != ab {
				return false
			}
		}
	}
	return true
}

func newClippedScene(w, h int, clip Path) (*image.RGBA, *ScannerGV, *Filler) {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	scanner := NewScannerGV(w, h, img, img.Bounds())
	scanner.SetColor(color.RGBA{255, 0, 0, 255})
	if clip != nil {
		scanner.SetClipPath(clip)
	}
	return img, scanner, NewFiller(w, h, scanner)
}

// TestClipPathCircle checks that a circular clip path crops a filled canvas:
// opaque inside, transparent outside, anti-aliased at the boundary, and that
// the mask is shared by consecutive draws without modifying the source path.
func TestClipPathCircle(t *testing.T) {
	clip := circlePath(50, 50, 30, false)
	orig := make(Path, len(clip))
	copy(orig, clip)

	img, _, f := newClippedScene(100, 100, clip)
	fillCanvas(f, 100, 100)

	if a := alphaOf(img, 50, 50); a != 255 {
		t.Errorf("center should be opaque, got alpha %d", a)
	}
	if a := alphaOf(img, 2, 2); a != 0 {
		t.Errorf("corner should be clipped away, got alpha %d", a)
	}
	// The boundary crosses y=50 at x=20 and x=80.
	if a := alphaOf(img, 25, 50); a != 255 {
		t.Errorf("well inside edge should be opaque, got alpha %d", a)
	}
	if a := alphaOf(img, 15, 50); a != 0 {
		t.Errorf("well outside edge should be transparent, got alpha %d", a)
	}
	// Anti-aliasing: the pixels straddling the boundary must be partial.
	a19, a20 := alphaOf(img, 19, 50), alphaOf(img, 20, 50)
	if !((a19 > 0 && a19 < 255) || (a20 > 0 && a20 < 255)) {
		t.Errorf("expected partial coverage at circle edge, got %d and %d", a19, a20)
	}

	// The clip path must not be modified by SetClipPath or Draw.
	if len(clip) != len(orig) {
		t.Fatalf("clip path length changed: %d -> %d", len(orig), len(clip))
	}
	for i := range orig {
		if clip[i] != orig[i] {
			t.Fatalf("clip path modified at index %d", i)
		}
	}

	// Consecutive draws share the same mask: a second fill is clipped too.
	fillCanvas(f, 100, 100)
	if a := alphaOf(img, 2, 2); a != 0 {
		t.Errorf("second draw should reuse the same mask, corner alpha %d", a)
	}
	if a := alphaOf(img, 50, 50); a != 255 {
		t.Errorf("second draw should still paint the center, alpha %d", a)
	}
}

// TestClipPathIntersectsRectClip checks the path clip is intersected with
// the rectangle clip set by SetClip.
func TestClipPathIntersectsRectClip(t *testing.T) {
	img, scanner, f := newClippedScene(100, 100, circlePath(50, 50, 15, false))
	scanner.SetClip(image.Rect(38, 38, 62, 62))
	fillCanvas(f, 100, 100)

	cases := []struct {
		x, y   int
		opaque bool
	}{
		{50, 50, true},  // inside both
		{45, 45, true},  // inside both
		{38, 38, false}, // inside rect, outside circle (nearest dist ~15.6 > 15)
		{50, 36, false}, // inside circle, outside rect
		{36, 50, false}, // inside circle, outside rect
		{70, 70, false}, // outside both
	}
	for _, c := range cases {
		a := alphaOf(img, c.x, c.y)
		if c.opaque && a != 255 {
			t.Errorf("(%d,%d) should be opaque, got alpha %d", c.x, c.y, a)
		}
		if !c.opaque && a != 0 {
			t.Errorf("(%d,%d) should be transparent, got alpha %d", c.x, c.y, a)
		}
	}
}

// TestClipPathReversedContour checks that reversing the winding direction of
// a contour clips the same region (non-zero winding rule).
func TestClipPathReversedContour(t *testing.T) {
	imgA, _, fA := newClippedScene(100, 100, circlePath(50, 50, 30, false))
	fillCanvas(fA, 100, 100)
	imgB, _, fB := newClippedScene(100, 100, circlePath(50, 50, 30, true))
	fillCanvas(fB, 100, 100)

	if !samePixels(imgA, imgB) {
		t.Error("reversed contour should clip identically to the forward contour")
	}
}

// TestClipPathNested checks nested contours: an oppositely wound inner
// contour punches a hole (donut), while a same-direction inner contour stays
// filled under the non-zero winding rule.
func TestClipPathNested(t *testing.T) {
	donut := circlePath(50, 50, 35, false)
	inner := circlePath(50, 50, 15, true) // reversed: subtracts the hole
	donut = append(donut, inner...)

	img, _, f := newClippedScene(100, 100, donut)
	fillCanvas(f, 100, 100)

	if a := alphaOf(img, 50, 50); a != 0 {
		t.Errorf("donut hole should be transparent, got alpha %d", a)
	}
	if a := alphaOf(img, 50, 25); a != 255 {
		t.Errorf("donut ring should be opaque, got alpha %d", a)
	}
	if a := alphaOf(img, 50, 10); a != 0 {
		t.Errorf("outside the donut should be transparent, got alpha %d", a)
	}

	// Same-direction nesting stays filled (non-zero winding).
	filled := circlePath(50, 50, 35, false)
	inner2 := circlePath(50, 50, 15, false)
	filled = append(filled, inner2...)
	img2, _, f2 := newClippedScene(100, 100, filled)
	fillCanvas(f2, 100, 100)
	if a := alphaOf(img2, 50, 50); a != 255 {
		t.Errorf("same-direction nested contour should stay filled, got alpha %d", a)
	}
}

// TestClipPathEmptyAndReset checks empty paths and clearing/replacing the
// clip path.
func TestClipPathEmptyAndReset(t *testing.T) {
	// A nil or empty path means no path clipping.
	img, scanner, f := newClippedScene(50, 50, nil)
	scanner.SetClipPath(nil)
	scanner.SetClipPath(Path{})
	fillCanvas(f, 50, 50)
	if a := alphaOf(img, 2, 2); a != 255 {
		t.Errorf("empty clip path should not clip, got alpha %d", a)
	}

	// A path with no filled area clips away everything.
	var moveOnly Path
	moveOnly.Start(ToFixedP(10, 10))
	moveOnly.Line(ToFixedP(20, 20))
	img2, _, f2 := newClippedScene(50, 50, moveOnly)
	fillCanvas(f2, 50, 50)
	if a := alphaOf(img2, 25, 25); a != 0 {
		t.Errorf("area-free clip path should clip everything, got alpha %d", a)
	}

	// Clearing the clip path restores unclipped rendering.
	img3, scanner3, f3 := newClippedScene(50, 50, circlePath(25, 25, 10, false))
	scanner3.ClearClipPath()
	fillCanvas(f3, 50, 50)
	if a := alphaOf(img3, 2, 2); a != 255 {
		t.Errorf("ClearClipPath should restore unclipped drawing, got alpha %d", a)
	}

	// Setting a new clip path replaces the previous one.
	img4, scanner4, f4 := newClippedScene(100, 50, circlePath(25, 25, 15, false))
	scanner4.SetClipPath(circlePath(75, 25, 15, false))
	fillCanvas(f4, 100, 50)
	if a := alphaOf(img4, 75, 25); a != 255 {
		t.Errorf("replacement clip path should apply, got alpha %d", a)
	}
	if a := alphaOf(img4, 25, 25); a != 0 {
		t.Errorf("replaced clip path should no longer apply, got alpha %d", a)
	}
}

// TestClipPathSelfIntersecting checks a self-intersecting (bowtie) clip path
// renders deterministically without a panic.
func TestClipPathSelfIntersecting(t *testing.T) {
	var bowtie Path
	bowtie.Start(ToFixedP(20, 20))
	bowtie.Line(ToFixedP(80, 80))
	bowtie.Line(ToFixedP(80, 20))
	bowtie.Line(ToFixedP(20, 80))
	bowtie.Stop(true)

	imgA, _, fA := newClippedScene(100, 100, bowtie)
	fillCanvas(fA, 100, 100)

	// The lobes of this bowtie are the left and right triangles.
	if a := alphaOf(imgA, 70, 50); a != 255 {
		t.Errorf("inside bowtie lobe should be opaque, got alpha %d", a)
	}
	if a := alphaOf(imgA, 30, 50); a != 255 {
		t.Errorf("inside bowtie lobe should be opaque, got alpha %d", a)
	}
	if a := alphaOf(imgA, 50, 25); a != 0 {
		t.Errorf("between bowtie lobes should be transparent, got alpha %d", a)
	}
	if a := alphaOf(imgA, 10, 10); a != 0 {
		t.Errorf("outside bowtie should be transparent, got alpha %d", a)
	}

	// Deterministic: rendering the same path again gives identical pixels.
	imgB, _, fB := newClippedScene(100, 100, bowtie)
	fillCanvas(fB, 100, 100)
	if !samePixels(imgA, imgB) {
		t.Error("self-intersecting clip path should render deterministically")
	}
}

// TestClipPathOutOfBounds checks clip paths with coordinates outside the
// canvas, including one entirely outside it.
func TestClipPathOutOfBounds(t *testing.T) {
	img, _, f := newClippedScene(100, 100, circlePath(-40, -40, 80, false))
	fillCanvas(f, 100, 100)
	if a := alphaOf(img, 5, 5); a != 255 {
		t.Errorf("region covered by off-canvas circle should be opaque, got alpha %d", a)
	}
	if a := alphaOf(img, 90, 90); a != 0 {
		t.Errorf("far corner should be transparent, got alpha %d", a)
	}

	// A clip path entirely outside the canvas clips everything away.
	img2, _, f2 := newClippedScene(100, 100, circlePath(500, 500, 50, false))
	fillCanvas(f2, 100, 100)
	if a := alphaOf(img2, 50, 50); a != 0 {
		t.Errorf("off-canvas clip path should clip everything, got alpha %d", a)
	}
}

// TestClipPathBezierEdge checks anti-aliased coverage along a quadratic
// bezier clip boundary.
func TestClipPathBezierEdge(t *testing.T) {
	var clip Path
	clip.Start(ToFixedP(10, 50))
	clip.QuadBezier(ToFixedP(50, -20), ToFixedP(90, 50)) // apex at y=15, x=50
	clip.Line(ToFixedP(90, 90))
	clip.Line(ToFixedP(10, 90))
	clip.Stop(true)

	img, _, f := newClippedScene(100, 100, clip)
	fillCanvas(f, 100, 100)

	if a := alphaOf(img, 50, 60); a != 255 {
		t.Errorf("below the curve should be opaque, got alpha %d", a)
	}
	if a := alphaOf(img, 50, 5); a != 0 {
		t.Errorf("above the curve should be transparent, got alpha %d", a)
	}
	// The boundary crosses x=50 near y=15: the alpha ramp must contain
	// partial values and be monotonically non-decreasing downward.
	prev := uint8(0)
	partials := 0
	for y := 10; y <= 20; y++ {
		a := alphaOf(img, 50, y)
		if a < prev {
			t.Errorf("alpha ramp not monotonic at y=%d: %d < %d", y, a, prev)
		}
		if a > 0 && a < 255 {
			partials++
		}
		prev = a
	}
	if partials == 0 {
		t.Error("expected anti-aliased partial coverage along bezier edge")
	}
	if prev != 255 {
		t.Errorf("ramp should end opaque, got %d", prev)
	}
}

// TestClipPathColorFunc checks path clipping composes with a ColorFunc source.
func TestClipPathColorFunc(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 100, 100))
	scanner := NewScannerGV(100, 100, img, img.Bounds())
	scanner.SetColor(ColorFunc(func(x, y int) color.Color {
		return color.RGBA{0, uint8(x * 2), 0, 255}
	}))
	scanner.SetClipPath(circlePath(50, 50, 30, false))
	f := NewFiller(100, 100, scanner)
	fillCanvas(f, 100, 100)

	if a := alphaOf(img, 2, 2); a != 0 {
		t.Errorf("ColorFunc source should be clipped, got alpha %d", a)
	}
	r, g, _, a := img.At(50, 50).RGBA()
	if a != 0xffff || r != 0 || g>>8 != 100 {
		t.Errorf("ColorFunc color wrong inside clip: r=%x g=%x a=%x", r, g, a)
	}
}

// TestClipPathLargeCanvas is a smoke test for a large canvas: the mask must
// be sized to the bounds, reused across SetClipPath calls, and not corrupt
// rendering.
func TestClipPathLargeCanvas(t *testing.T) {
	const w, h = 2000, 1500
	img, scanner, f := newClippedScene(w, h, circlePath(w/2, h/2, 400, false))
	// Re-set the same-size clip path; the mask buffer is reused.
	scanner.SetClipPath(circlePath(w/2, h/2, 400, false))
	fillCanvas(f, w, h)

	if a := alphaOf(img, w/2, h/2); a != 255 {
		t.Errorf("center should be opaque, got alpha %d", a)
	}
	if a := alphaOf(img, 5, 5); a != 0 {
		t.Errorf("corner should be transparent, got alpha %d", a)
	}
	if a := alphaOf(img, w-5, h-5); a != 0 {
		t.Errorf("far corner should be transparent, got alpha %d", a)
	}
}

// TestNoClipPathUnchanged checks that without a clip path, rendering is
// exactly as before: a full-canvas fill paints every pixel, and a rectangle
// clip alone still works.
func TestNoClipPathUnchanged(t *testing.T) {
	img, _, f := newClippedScene(60, 60, nil)
	fillCanvas(f, 60, 60)
	for y := 0; y < 60; y++ {
		for x := 0; x < 60; x++ {
			if a := alphaOf(img, x, y); a != 255 {
				t.Fatalf("unclipped fill should be opaque everywhere, (%d,%d) alpha %d", x, y, a)
			}
		}
	}

	img2, scanner2, f2 := newClippedScene(60, 60, nil)
	scanner2.SetClip(image.Rect(10, 10, 20, 20))
	fillCanvas(f2, 60, 60)
	if a := alphaOf(img2, 15, 15); a != 255 {
		t.Errorf("rect clip should paint inside, got alpha %d", a)
	}
	if a := alphaOf(img2, 25, 25); a != 0 {
		t.Errorf("rect clip should clip outside, got alpha %d", a)
	}
}
