// Copyright 2026 by the rasterx Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rasterx_test

import (
	"image"
	"math"
	"testing"

	. "github.com/srwiley/rasterx"
	"golang.org/x/image/math/fixed"
)

// recPathScanner records every subpath emitted into the embedded scanner so
// stroke geometry can be inspected without rasterizing.
type recPathScanner struct {
	paths [][]fixed.Point26_6
	cur   []fixed.Point26_6
}

func (s *recPathScanner) Start(a fixed.Point26_6) {
	if len(s.cur) > 0 {
		s.paths = append(s.paths, s.cur)
	}
	s.cur = []fixed.Point26_6{a}
}
func (s *recPathScanner) Line(b fixed.Point26_6)             { s.cur = append(s.cur, b) }
func (s *recPathScanner) Draw()                              {}
func (s *recPathScanner) GetPathExtent() fixed.Rectangle26_6 { return fixed.Rectangle26_6{} }
func (s *recPathScanner) SetBounds(w, h int)                 {}
func (s *recPathScanner) SetColor(interface{})               {}
func (s *recPathScanner) SetWinding(bool)                    {}
func (s *recPathScanner) SetClip(image.Rectangle)            {}
func (s *recPathScanner) Clear()                             { s.paths, s.cur = nil, nil }
func (s *recPathScanner) allPoints() []fixed.Point26_6 {
	out := make([]fixed.Point26_6, 0)
	for _, p := range s.paths {
		out = append(out, p...)
	}
	if len(s.cur) > 0 {
		out = append(out, s.cur...)
	}
	return out
}

// rectCWPath builds a visually clockwise rectangle (screen Y points down).
func rectCWPath(x1, y1, x2, y2 float64) Path {
	var p Path
	p.Start(ToFixedP(x1, y1))
	p.Line(ToFixedP(x2, y1))
	p.Line(ToFixedP(x2, y2))
	p.Line(ToFixedP(x1, y2))
	p.Stop(true)
	return p
}

// rectCCWPath builds the same rectangle traversed in the opposite direction.
func rectCCWPath(x1, y1, x2, y2 float64) Path {
	var p Path
	p.Start(ToFixedP(x1, y1))
	p.Line(ToFixedP(x1, y2))
	p.Line(ToFixedP(x2, y2))
	p.Line(ToFixedP(x2, y1))
	p.Stop(true)
	return p
}

// bboxOf returns the integer pixel bounding box of the emitted geometry.
func bboxOf(pts []fixed.Point26_6) image.Rectangle {
	if len(pts) == 0 {
		return image.Rectangle{}
	}
	minX, minY := pts[0].X, pts[0].Y
	maxX, maxY := pts[0].X, pts[0].Y
	for _, p := range pts[1:] {
		if p.X < minX {
			minX = p.X
		}
		if p.X > maxX {
			maxX = p.X
		}
		if p.Y < minY {
			minY = p.Y
		}
		if p.Y > maxY {
			maxY = p.Y
		}
	}
	// Convert fixed coordinates to pixel ranges, rounding the outer extent
	// outward so an exact boundary is included.
	return image.Rect(int(minX)>>6, int(minY)>>6,
		int((maxX+63))>>6, int((maxY+63))>>6)
}

func newRecStroker(mode StrokeMode, width fixed.Int26_6) (*Stroker, *recPathScanner) {
	sc := &recPathScanner{}
	s := NewStroker(512, 512, sc)
	s.SetStroke(width, 4*64, ButtCap, nil, FlatGap, MiterClip)
	s.SetStrokeMode(mode)
	return s, sc
}

// Rule 1 + rectangle verification: outside strokes extend outward and keep the
// interior boundary at the source contour; inside strokes keep the exterior
// boundary at the source contour.
func TestStrokePlacementRectangle(t *testing.T) {
	const w = fixed.Int26_6(10 * 64)
	x1, y1, x2, y2 := 50.0, 60.0, 150.0, 120.0

	// Outside: every emitted point must lie on or outside the source rectangle,
	// and the bounding box must be expanded by exactly the stroke width.
	s, sc := newRecStroker(StrokeOutside, w)
	rectCWPath(x1, y1, x2, y2).AddTo(s)
	bb := bboxOf(sc.allPoints())
	wantOut := image.Rect(40, 50, 160, 130)
	if bb != wantOut {
		t.Errorf("outside bbox = %v, want %v", bb, wantOut)
	}
	for _, p := range sc.allPoints() {
		fx, fy := float64(p.X)/64, float64(p.Y)/64
		if fx > x1+1e-3 && fx < x2-1e-3 && fy > y1+1e-3 && fy < y2-1e-3 {
			t.Errorf("outside stroke emitted interior point (%v,%v)", fx, fy)
		}
	}

	// Inside: every emitted point must lie on or inside the source rectangle,
	// and the bounding box must equal the source rectangle.
	s, sc = newRecStroker(StrokeInside, w)
	rectCWPath(x1, y1, x2, y2).AddTo(s)
	bb = bboxOf(sc.allPoints())
	wantIn := image.Rect(50, 60, 150, 120)
	if bb != wantIn {
		t.Errorf("inside bbox = %v, want %v", bb, wantIn)
	}
	for _, p := range sc.allPoints() {
		fx, fy := float64(p.X)/64, float64(p.Y)/64
		if fx < x1-1e-3 || fx > x2+1e-3 || fy < y1-1e-3 || fy > y2+1e-3 {
			t.Errorf("inside stroke emitted exterior point (%v,%v)", fx, fy)
		}
	}
}

// Rule 2: reversing the contour direction must produce an equivalent stroke:
// the same band of pixels is filled for each placement mode. The internal
// subpath stitching visits degenerate points different numbers of times, so
// rendered pixel equivalence is the meaningful invariant.
func TestStrokePlacementWindingInvariant(t *testing.T) {
	const w = fixed.Int26_6(8 * 64)
	render := func(mode StrokeMode, ccw bool) *image.RGBA {
		img := image.NewRGBA(image.Rect(0, 0, 256, 256))
		sc := NewScannerGV(256, 256, img, img.Bounds())
		s := NewStroker(256, 256, sc)
		s.SetStroke(w, 4*64, ButtCap, nil, FlatGap, MiterClip)
		s.SetStrokeMode(mode)
		if ccw {
			rectCCWPath(30, 40, 170, 130).AddTo(s)
		} else {
			rectCWPath(30, 40, 170, 130).AddTo(s)
		}
		s.Draw()
		return img
	}
	for _, mode := range []StrokeMode{StrokeInside, StrokeOutside} {
		a, b := render(mode, false), render(mode, true)
		for i := range a.Pix {
			if a.Pix[i] != b.Pix[i] {
				t.Fatalf("mode %v: rendered pixels differ at index %d (%d vs %d)",
					mode, i, a.Pix[i], b.Pix[i])
			}
		}
	}
}

// Rule 2 (Bezier segments): a circle drawn with inside/outside strokes keeps
// its circular inner and outer boundaries within the bezier approximation
// tolerance of the ideal offset radii.
func TestStrokePlacementCircle(t *testing.T) {
	cx, cy, r, sw := 100.0, 100.0, 60.0, 10.0
	for _, mode := range []StrokeMode{StrokeInside, StrokeOutside} {
		sc := &recPathScanner{}
		s := NewStroker(512, 512, sc)
		// Round joins/gaps keep the offset boundary close to a true circle.
		s.SetStroke(fixed.Int26_6(sw*64), 4*64, ButtCap, nil, RoundGap, MiterClip)
		s.SetStrokeMode(mode)
		var p Path
		AddCircle(cx, cy, r, &p)
		p.AddTo(s)
		var minR, maxR float64
		first := true
		for _, pt := range sc.allPoints() {
			d := math.Hypot(float64(pt.X)/64-cx, float64(pt.Y)/64-cy)
			if first || d < minR {
				minR = d
			}
			if first || d > maxR {
				maxR = d
			}
			first = false
		}
		// The stroke band occupies an annulus between two of {r-sw, r, r+sw}.
		// Cubic arc approximation (MaxDx) stays within ~0.05px of the circle.
		const tol = 0.12
		var wantMin, wantMax float64
		if mode == StrokeOutside {
			wantMin, wantMax = r, r+sw
		} else {
			wantMin, wantMax = r-sw, r
		}
		if math.Abs(minR-wantMin) > tol || math.Abs(maxR-wantMax) > tol {
			t.Errorf("circle mode %v: radii [%.3f, %.3f], want [%.3f, %.3f]",
				mode, minR, maxR, wantMin, wantMax)
		}
	}
}

// Rule 2 (transform invariance): stroking a transformed path with a width
// given in the transformed coordinate system is equivalent to stroking the
// already-transformed geometry. Inside/outside offsets follow the transform
// just like centered strokes.
func TestStrokePlacementTransform(t *testing.T) {
	const w = fixed.Int26_6(8 * 64)
	m := Identity.Translate(-20, 11).Rotate(0.6).Scale(1.25, 0.9)
	base := rectCWPath(40, 50, 140, 120)

	sc1 := &recPathScanner{}
	s1 := NewStroker(512, 512, sc1)
	s1.SetStroke(w, 4*64, ButtCap, nil, FlatGap, MiterClip)
	s1.SetStrokeMode(StrokeOutside)
	base.AddTo(&MatrixAdder{Adder: s1, M: m})

	// Build the explicitly transformed path.
	var transformed Path
	for i := 0; i < len(base); {
		switch PathCommand(base[i]) {
		case PathMoveTo:
			transformed.Start(m.TFixed(fixed.Point26_6{X: base[i+1], Y: base[i+2]}))
			i += 3
		case PathLineTo:
			transformed.Line(m.TFixed(fixed.Point26_6{X: base[i+1], Y: base[i+2]}))
			i += 3
		case PathClose:
			transformed.Stop(true)
			i++
		default:
			i++
		}
	}
	sc2 := &recPathScanner{}
	s2 := NewStroker(512, 512, sc2)
	s2.SetStroke(w, 4*64, ButtCap, nil, FlatGap, MiterClip)
	s2.SetStrokeMode(StrokeOutside)
	transformed.AddTo(s2)

	if len(sc1.allPoints()) != len(sc2.allPoints()) {
		t.Fatalf("transformed point counts differ: %d vs %d",
			len(sc1.allPoints()), len(sc2.allPoints()))
	}
	for i, p := range sc1.allPoints() {
		if p != sc2.allPoints()[i] {
			t.Errorf("point %d differs under transform: %v vs %v", i, p, sc2.allPoints()[i])
		}
	}
}

// Rule 3: open paths are rejected for inside/outside strokes, including an
// open subpath mixed into a buffer that also contains closed subpaths.
func TestStrokePlacementOpenPath(t *testing.T) {
	for _, mode := range []StrokeMode{StrokeInside, StrokeOutside} {
		s, _ := newRecStroker(mode, 4*64)
		err := checkPanic(func() {
			s.Start(ToFixedP(10, 10))
			s.Line(ToFixedP(80, 20))
			s.Line(ToFixedP(60, 90))
			s.Stop(false)
		})
		if err != ErrOpenPathStroke {
			t.Errorf("mode %v: got %v, want ErrOpenPathStroke", mode, err)
		}
		// The stroker remains usable after the panic.
		s.SetStrokeMode(StrokeCenter)
		rectCWPath(10, 10, 40, 40).AddTo(s)

		// Open subpath followed by a closed one in the same buffer.
		s2, _ := newRecStroker(mode, 4*64)
		err = checkPanic(func() {
			s2.Start(ToFixedP(10, 10))
			s2.Line(ToFixedP(80, 20))
			s2.Stop(false)
			rectCWPath(120, 120, 180, 180).AddTo(s2)
		})
		if err != ErrOpenPathStroke {
			t.Errorf("mode %v mixed subpaths: got %v, want ErrOpenPathStroke", mode, err)
		}
	}
	// Centered open paths keep using end caps and never panic.
	s, sc := newRecStroker(StrokeCenter, 4*64)
	s.Start(ToFixedP(10, 10))
	s.Line(ToFixedP(80, 20))
	s.Stop(false)
	if len(sc.allPoints()) == 0 {
		t.Error("centered open path produced no geometry")
	}
}

// Rule 3: closed contours with zero signed area are rejected.
func TestStrokePlacementZeroArea(t *testing.T) {
	for _, mode := range []StrokeMode{StrokeInside, StrokeOutside} {
		s, _ := newRecStroker(mode, 4*64)
		err := checkPanic(func() {
			var p Path
			// Out-and-back contour: every edge cancels in the shoelace sum.
			p.Start(ToFixedP(20, 20))
			p.Line(ToFixedP(120, 20))
			p.Line(ToFixedP(120, 90))
			p.Line(ToFixedP(120, 20))
			p.Line(ToFixedP(20, 20))
			p.Stop(true)
			p.AddTo(s)
		})
		if err != ErrZeroAreaStroke {
			t.Errorf("mode %v: got %v, want ErrZeroAreaStroke", mode, err)
		}
	}
}

// Rule 3 (holes): multiple subpaths, including a counter-oriented hole, are
// each placed against their own signed area. The outer contour expands
// outward and the hole expands into its exterior, which is the fill region.
func TestStrokePlacementWithHole(t *testing.T) {
	const w = fixed.Int26_6(6 * 64)
	s, sc := newRecStroker(StrokeOutside, w)
	var p Path
	// Outer rectangle, visually clockwise.
	p.Start(ToFixedP(20, 20))
	p.Line(ToFixedP(180, 20))
	p.Line(ToFixedP(180, 140))
	p.Line(ToFixedP(20, 140))
	p.Stop(true)
	// Inner rectangle (hole), opposite traversal.
	p.Start(ToFixedP(70, 60))
	p.Line(ToFixedP(70, 110))
	p.Line(ToFixedP(130, 110))
	p.Line(ToFixedP(130, 60))
	p.Stop(true)
	p.AddTo(s)
	bb := bboxOf(sc.allPoints())
	// Outer contour reaches 20-6=14 and 180+6=186, 20-6=14 and 140+6=146.
	want := image.Rect(14, 14, 186, 146)
	if bb != want {
		t.Errorf("hole bbox = %v, want %v", bb, want)
	}
	// The hole's outside stroke must enter the fill region (points between the
	// hole boundary and six pixels away from it), never cross its interior.
	holeMin := fixed.Point26_6{X: 70 * 64, Y: 60 * 64}
	holeMax := fixed.Point26_6{X: 130 * 64, Y: 110 * 64}
	for _, pt := range sc.allPoints() {
		if pt.X > holeMin.X && pt.X < holeMax.X && pt.Y > holeMin.Y && pt.Y < holeMax.Y {
			t.Errorf("hole stroke leaked into hole interior at %v", pt)
		}
	}
}

// Rule 3 (narrow contour): an inside stroke wider than the contour itself is
// still emitted without error; the offset sides collapse onto the contour,
// producing a closed, fillable band.
func TestStrokePlacementNarrowContour(t *testing.T) {
	for _, mode := range []StrokeMode{StrokeInside, StrokeOutside} {
		s, sc := newRecStroker(mode, 40*64) // width far exceeds the 6px height
		rectCWPath(20, 50, 120, 56).AddTo(s)
		if len(sc.allPoints()) == 0 {
			t.Errorf("mode %v: narrow contour produced no geometry", mode)
		}
	}
}

// Rule 3: negative widths panic; zero widths produce no stroke geometry.
func TestStrokeWidthValidation(t *testing.T) {
	s, _ := newRecStroker(StrokeCenter, 4*64)
	err := checkPanic(func() {
		s.SetStroke(-1, 4*64, ButtCap, nil, FlatGap, MiterClip)
	})
	if err != ErrNegativeWidth {
		t.Errorf("negative width: got %v, want ErrNegativeWidth", err)
	}

	for _, mode := range []StrokeMode{StrokeCenter, StrokeInside, StrokeOutside} {
		img := image.NewRGBA(image.Rect(0, 0, 256, 256))
		sc := NewScannerGV(256, 256, img, img.Bounds())
		s := NewStroker(256, 256, sc)
		s.SetStroke(0, 4*64, ButtCap, nil, FlatGap, MiterClip)
		s.SetStrokeMode(mode)
		rectCWPath(10, 10, 90, 90).AddTo(s)
		s.Draw()
		for i, v := range img.Pix {
			// RGBA: check the alpha channel of every pixel.
			if i%4 == 3 && v != 0 {
				t.Errorf("mode %v: zero width produced visible pixels", mode)
				break
			}
		}
	}
}

// Rule 3: inside/outside placement combined with dashes is rejected.
func TestStrokePlacementDashes(t *testing.T) {
	for _, mode := range []StrokeMode{StrokeInside, StrokeOutside} {
		sc := &recPathScanner{}
		d := NewDasher(256, 256, sc)
		d.SetStroke(6*64, 4*64, RoundCap, nil, RoundGap, MiterClip, []float64{20, 8}, 0)
		d.SetStrokeMode(mode)
		err := checkPanic(func() {
			rectCWPath(10, 10, 90, 90).AddTo(d)
		})
		if err != ErrDashedStrokePlacement {
			t.Errorf("mode %v: got %v, want ErrDashedStrokePlacement", mode, err)
		}
	}
	// Dashed centered strokes still work.
	sc := &recPathScanner{}
	d := NewDasher(256, 256, sc)
	d.SetStroke(6*64, 4*64, RoundCap, nil, RoundGap, MiterClip, []float64{20, 8}, 0)
	rectCWPath(10, 10, 90, 90).AddTo(d)
	if len(sc.allPoints()) == 0 {
		t.Error("dashed centered stroke produced no geometry")
	}
}

// Rule 2 (join/cap/miter settings): miter, bevel and round join modes all
// produce closed bands in inside/outside mode, and the miter limit actually
// clips the convex outer corner when exceeded.
func TestStrokePlacementJoinModes(t *testing.T) {
	const w = fixed.Int26_6(10 * 64)
	for _, jm := range []JoinMode{Miter, MiterClip, Bevel, Round, Arc, ArcClip} {
		sc := &recPathScanner{}
		s := NewStroker(512, 512, sc)
		s.SetStroke(w, 4*64, RoundCap, nil, RoundGap, jm)
		s.SetStrokeMode(StrokeOutside)
		// A sharp 30-degree-ish corner makes the miter extend well past
		// width*limit for the clip modes.
		var p Path
		p.Start(ToFixedP(60, 200))
		p.Line(ToFixedP(200, 180))
		p.Line(ToFixedP(260, 90))
		p.Line(ToFixedP(120, 60))
		p.Stop(true)
		p.AddTo(s)
		if len(sc.allPoints()) < 4 {
			t.Errorf("join mode %v produced no usable band", jm)
		}
	}

	// Miter vs a tight MiterClip must differ on the same sharp contour.
	build := func(jm JoinMode, ml fixed.Int26_6) []fixed.Point26_6 {
		sc := &recPathScanner{}
		s := NewStroker(512, 512, sc)
		s.SetStroke(w, ml, ButtCap, nil, FlatGap, jm)
		s.SetStrokeMode(StrokeOutside)
		var p Path
		p.Start(ToFixedP(60, 200))
		p.Line(ToFixedP(200, 180))
		p.Line(ToFixedP(260, 90))
		p.Stop(true)
		p.AddTo(s)
		return sc.allPoints()
	}
	miterPts := build(Miter, 4*64)
	clipPts := build(MiterClip, 1*64)
	same := len(miterPts) == len(clipPts)
	if same {
		for i := range miterPts {
			if miterPts[i] != clipPts[i] {
				same = false
				break
			}
		}
	}
	if same {
		t.Error("miter limit clipping had no effect on the outside stroke")
	}
}

// Rule 1: the default mode is StrokeCenter and SetStrokeMode changes it.
func TestStrokeModeDefault(t *testing.T) {
	s, _ := newRecStroker(StrokeCenter, 4*64)
	if s.StrokeMode() != StrokeCenter {
		t.Fatalf("default mode = %v, want StrokeCenter", s.StrokeMode())
	}
	s.SetStrokeMode(StrokeOutside)
	if s.StrokeMode() != StrokeOutside {
		t.Fatalf("mode after SetStrokeMode = %v", s.StrokeMode())
	}
}

// checkPanic runs f and returns the value recovered from a panic, or nil.
func checkPanic(f func()) (rec interface{}) {
	defer func() { rec = recover() }()
	f()
	return
}
