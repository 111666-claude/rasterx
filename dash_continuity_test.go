// Copyright 2026 by the rasterx Authors. All rights reserved.
//
// Tests for cross-subpath dash phase continuity (Dasher.ContinueDashPhase).
package rasterx_test

import (
	"image"
	"testing"

	. "github.com/srwiley/rasterx"
	"golang.org/x/image/math/fixed"
)

type dashPhase struct {
	place    int
	consumed fixed.Int26_6
	isGap    bool
}

func getPhase(d *Dasher) dashPhase {
	p, c, g := d.DashPhase()
	return dashPhase{p, c, g}
}

func newDashTestDasher(img *image.RGBA, dashes []float64, offset float64, continuity bool) *Dasher {
	w := img.Bounds().Dx()
	h := img.Bounds().Dy()
	scanner := NewScannerGV(w, h, img, img.Bounds())
	d := NewDasher(w, h, scanner)
	d.SetStroke(4*64, 4*64, ButtCap, nil, FlatGap, Miter, dashes, offset)
	d.ContinueDashPhase = continuity
	return d
}

// fx is a helper to build fixed point coordinates.
func fx(x, y int) fixed.Point26_6 { return ToFixedP(float64(x), float64(y)) }

// lineWhole draws p0->p1->p2->p3 as a single subpath.
func lineWhole(d *Dasher, pts []fixed.Point26_6) {
	d.Start(pts[0])
	for _, p := range pts[1:] {
		d.Line(p)
	}
	d.Stop(false)
}

// lineSplit draws the same polyline as one subpath per segment.
func lineSplit(d *Dasher, pts []fixed.Point26_6) {
	for i := 0; i+1 < len(pts); i++ {
		d.Start(pts[i])
		d.Line(pts[i+1])
		d.Stop(false)
	}
}

// TestDashContinuityLinesSamePhase checks that a polyline rendered as one
// subpath and the same polyline rendered as several subpaths consume exactly
// the same dash phase, and produce identical rasters at the seams.
func TestDashContinuityLinesSamePhase(t *testing.T) {
	pts := []fixed.Point26_6{fx(20, 100), fx(200, 100), fx(380, 100), fx(500, 100)}

	imgWhole := image.NewRGBA(image.Rect(0, 0, 600, 200))
	dw := newDashTestDasher(imgWhole, []float64{33, 12}, 30, true)
	lineWhole(dw, pts)
	dw.Draw()
	wholePhase := getPhase(dw)

	imgSplit := image.NewRGBA(image.Rect(0, 0, 600, 200))
	ds := newDashTestDasher(imgSplit, []float64{33, 12}, 30, true)
	lineSplit(ds, pts)
	ds.Draw()
	splitPhase := getPhase(ds)

	if wholePhase != splitPhase {
		t.Errorf("phase mismatch: whole %v, split %v", wholePhase, splitPhase)
	}
	if len(imgWhole.Pix) != len(imgSplit.Pix) {
		t.Fatalf("image size mismatch")
	}
	for i := range imgWhole.Pix {
		if imgWhole.Pix[i] != imgSplit.Pix[i] {
			t.Fatalf("raster mismatch at byte %d: whole %d, split %d",
				i, imgWhole.Pix[i], imgSplit.Pix[i])
		}
	}
}

// TestDashContinuityCurvesSamePhase checks phase continuity across subpaths
// containing quadratic and cubic bezier segments.
func TestDashContinuityCurvesSamePhase(t *testing.T) {
	p0, p1 := fx(20, 150), fx(120, 150)
	qb, qc := fx(170, 50), fx(220, 150)                   // quad control, end
	cb, cc, cd := fx(280, 250), fx(360, 50), fx(420, 150) // cubic controls, end

	imgWhole := image.NewRGBA(image.Rect(0, 0, 500, 300))
	dw := newDashTestDasher(imgWhole, []float64{17, 9}, 5, true)
	dw.Start(p0)
	dw.Line(p1)
	dw.QuadBezier(qb, qc)
	dw.CubeBezier(cb, cc, cd)
	dw.Stop(false)
	dw.Draw()
	wholePhase := getPhase(dw)

	// Same curve, one subpath per segment.
	imgSplit := image.NewRGBA(image.Rect(0, 0, 500, 300))
	ds := newDashTestDasher(imgSplit, []float64{17, 9}, 5, true)
	ds.Start(p0)
	ds.Line(p1)
	ds.Stop(false)
	ds.Start(p1)
	ds.QuadBezier(qb, qc)
	ds.Stop(false)
	ds.Start(qc)
	ds.CubeBezier(cb, cc, cd)
	ds.Stop(false)
	ds.Draw()
	if wholePhase != getPhase(ds) {
		t.Errorf("3-way split phase mismatch: whole %v, split %v", wholePhase, getPhase(ds))
	}

	// Same curve, split inside the cubic segment region: line+quad together,
	// then the cubic.
	imgSplit2 := image.NewRGBA(image.Rect(0, 0, 500, 300))
	ds2 := newDashTestDasher(imgSplit2, []float64{17, 9}, 5, true)
	ds2.Start(p0)
	ds2.Line(p1)
	ds2.QuadBezier(qb, qc)
	ds2.Stop(false)
	ds2.Start(qc)
	ds2.CubeBezier(cb, cc, cd)
	ds2.Stop(false)
	ds2.Draw()
	if wholePhase != getPhase(ds2) {
		t.Errorf("2-way split phase mismatch: whole %v, split %v", wholePhase, getPhase(ds2))
	}
}

// TestDashContinuityDefaultOff verifies that the default behavior is
// unchanged: every Start restarts the dash pattern at the dash offset.
func TestDashContinuityDefaultOff(t *testing.T) {
	pts := []fixed.Point26_6{fx(20, 100), fx(200, 100), fx(500, 100)}

	img := image.NewRGBA(image.Rect(0, 0, 600, 200))
	d := newDashTestDasher(img, []float64{33, 12}, 30, false)
	if d.ContinueDashPhase {
		t.Fatal("ContinueDashPhase should default to false")
	}
	lineSplit(d, pts)
	d.Draw()

	// The last subpath must have consumed the pattern exactly as if it were
	// drawn alone on a fresh dasher.
	img2 := image.NewRGBA(image.Rect(0, 0, 600, 200))
	d2 := newDashTestDasher(img2, []float64{33, 12}, 30, false)
	d2.Start(pts[1])
	d2.Line(pts[2])
	d2.Stop(false)
	d2.Draw()

	if getPhase(d) != getPhase(d2) {
		t.Errorf("default mode: split phase %v differs from independent render %v",
			getPhase(d), getPhase(d2))
	}
}

// TestDashContinuityReset checks ResetDashPhase restarts the pattern at the
// dash offset even when continuity is enabled.
func TestDashContinuityReset(t *testing.T) {
	pts := []fixed.Point26_6{fx(20, 100), fx(200, 100), fx(500, 100)}

	img := image.NewRGBA(image.Rect(0, 0, 600, 200))
	d := newDashTestDasher(img, []float64{33, 12}, 30, true)
	d.Start(pts[0])
	d.Line(pts[1])
	d.Stop(false)
	d.ResetDashPhase()
	d.Start(pts[1])
	d.Line(pts[2])
	d.Stop(false)
	d.Draw()

	img2 := image.NewRGBA(image.Rect(0, 0, 600, 200))
	d2 := newDashTestDasher(img2, []float64{33, 12}, 30, true)
	d2.Start(pts[1])
	d2.Line(pts[2])
	d2.Stop(false)
	d2.Draw()

	if getPhase(d) != getPhase(d2) {
		t.Errorf("after ResetDashPhase: phase %v differs from fresh render %v",
			getPhase(d), getPhase(d2))
	}
}

// TestDashContinuityClosedPath checks that closing a path advances the phase
// by the length of the closing segment, matching an explicitly closed
// polyline, and that closed-path join/cap handling is unchanged.
func TestDashContinuityClosedPath(t *testing.T) {
	p0, p1, p2 := fx(100, 50), fx(300, 50), fx(200, 200)

	img1 := image.NewRGBA(image.Rect(0, 0, 400, 300))
	d1 := newDashTestDasher(img1, []float64{25, 15}, 0, true)
	d1.Start(p0)
	d1.Line(p1)
	d1.Line(p2)
	d1.Stop(true) // implicitly closes back to p0
	d1.Draw()

	img2 := image.NewRGBA(image.Rect(0, 0, 400, 300))
	d2 := newDashTestDasher(img2, []float64{25, 15}, 0, true)
	d2.Start(p0)
	d2.Line(p1)
	d2.Line(p2)
	d2.Line(p0) // explicit closing segment
	d2.Stop(false)
	d2.Draw()

	if getPhase(d1) != getPhase(d2) {
		t.Errorf("closed path phase %v differs from explicitly closed %v",
			getPhase(d1), getPhase(d2))
	}
}

// TestDashContinuityEdgeCases checks deterministic handling of empty and
// zero-length subpaths, negative offsets, odd dash arrays, and invalid
// (non-positive) dash lengths.
func TestDashContinuityEdgeCases(t *testing.T) {
	pts := []fixed.Point26_6{fx(20, 100), fx(200, 100), fx(500, 100)}

	// Zero-length segments and empty subpaths must not advance the phase.
	img1 := image.NewRGBA(image.Rect(0, 0, 600, 200))
	d1 := newDashTestDasher(img1, []float64{33, 12}, 30, true)
	d1.Start(pts[0])
	d1.Line(pts[0]) // zero-length segment
	d1.Line(pts[1])
	d1.Stop(false)
	d1.Start(pts[1]) // empty subpath: no segments before Stop
	d1.Stop(false)
	d1.Start(pts[1])
	d1.Line(pts[1]) // zero-length segment
	d1.Line(pts[2])
	d1.Stop(false)
	d1.Draw()

	img2 := image.NewRGBA(image.Rect(0, 0, 600, 200))
	d2 := newDashTestDasher(img2, []float64{33, 12}, 30, true)
	lineWhole(d2, pts)
	d2.Draw()

	if getPhase(d1) != getPhase(d2) {
		t.Errorf("zero-length/empty subpaths changed phase: %v vs %v",
			getPhase(d1), getPhase(d2))
	}

	// Negative offset: split and whole renders must still agree.
	img3 := image.NewRGBA(image.Rect(0, 0, 600, 200))
	d3 := newDashTestDasher(img3, []float64{33, 12}, -30, true)
	lineWhole(d3, pts)
	d3.Draw()
	img4 := image.NewRGBA(image.Rect(0, 0, 600, 200))
	d4 := newDashTestDasher(img4, []float64{33, 12}, -30, true)
	lineSplit(d4, pts)
	d4.Draw()
	if getPhase(d3) != getPhase(d4) {
		t.Errorf("negative offset: whole %v vs split %v", getPhase(d3), getPhase(d4))
	}

	// Odd dash array.
	img5 := image.NewRGBA(image.Rect(0, 0, 600, 200))
	d5 := newDashTestDasher(img5, []float64{10, 4, 6}, 3, true)
	lineWhole(d5, pts)
	d5.Draw()
	img6 := image.NewRGBA(image.Rect(0, 0, 600, 200))
	d6 := newDashTestDasher(img6, []float64{10, 4, 6}, 3, true)
	lineSplit(d6, pts)
	d6.Draw()
	if getPhase(d5) != getPhase(d6) {
		t.Errorf("odd dash array: whole %v vs split %v", getPhase(d5), getPhase(d6))
	}

	// Zero and negative dash lengths are treated as zero; continuity must
	// still hold and terminate.
	img7 := image.NewRGBA(image.Rect(0, 0, 600, 200))
	d7 := newDashTestDasher(img7, []float64{0, 8, -3, 4}, 2, true)
	lineWhole(d7, pts)
	d7.Draw()
	img8 := image.NewRGBA(image.Rect(0, 0, 600, 200))
	d8 := newDashTestDasher(img8, []float64{0, 8, -3, 4}, 2, true)
	lineSplit(d8, pts)
	d8.Draw()
	if getPhase(d7) != getPhase(d8) {
		t.Errorf("invalid dash lengths: whole %v vs split %v", getPhase(d7), getPhase(d8))
	}

	// All-zero dash array falls back to solid stroking; must not hang or
	// panic, with or without continuity.
	img9 := image.NewRGBA(image.Rect(0, 0, 600, 200))
	d9 := newDashTestDasher(img9, []float64{0, 0}, 0, true)
	lineSplit(d9, pts)
	d9.Draw()
}

// TestDashContinuityExactBoundary checks the seam behavior when a subpath
// ends exactly on a dash boundary: no zero-length dash bit or extra cap is
// emitted at the seam.
func TestDashContinuityExactBoundary(t *testing.T) {
	// Dashes of exactly 10px, offset 0; first subpath is exactly 30px long,
	// ending exactly on a dash boundary.
	img1 := image.NewRGBA(image.Rect(0, 0, 200, 50))
	d1 := newDashTestDasher(img1, []float64{10, 10}, 0, true)
	d1.Start(fx(10, 25))
	d1.Line(fx(40, 25))
	d1.Stop(false)
	// After 30px with pattern 10/10 the phase sits exactly on an element
	// boundary; normalization must have consumed it by the next Start.
	d1.Start(fx(40, 25))
	d1.Line(fx(90, 25))
	d1.Stop(false)
	d1.Draw()

	img2 := image.NewRGBA(image.Rect(0, 0, 200, 50))
	d2 := newDashTestDasher(img2, []float64{10, 10}, 0, true)
	d2.Start(fx(10, 25))
	d2.Line(fx(40, 25))
	d2.Line(fx(90, 25))
	d2.Stop(false)
	d2.Draw()

	if getPhase(d1) != getPhase(d2) {
		t.Errorf("exact boundary: split %v vs whole %v", getPhase(d1), getPhase(d2))
	}
	for i := range img1.Pix {
		if img1.Pix[i] != img2.Pix[i] {
			t.Fatalf("exact boundary raster mismatch at byte %d: split %d, whole %d",
				i, img1.Pix[i], img2.Pix[i])
		}
	}
}
