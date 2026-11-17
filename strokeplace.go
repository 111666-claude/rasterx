// Copyright 2026 by the rasterx Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rasterx

import (
	"math"

	"golang.org/x/image/math/fixed"
)

// zeroAreaTol is the absolute signed area (in pixels squared) below which a
// closed contour is treated as having zero area. Bezier flattening lands on a
// fixed point grid, so genuinely empty contours accumulate only rounding noise.
const zeroAreaTol = 1e-3

// splitSubpaths splits a flat Path token stream into its component subpaths.
// Each returned subpath begins with a PathMoveTo. A trailing PathClose is kept
// on the subpath it closes.
func splitSubpaths(p Path) []Path {
	var subs []Path
	var cur Path
	for i := 0; i < len(p); {
		switch PathCommand(p[i]) {
		case PathMoveTo:
			if len(cur) > 0 {
				subs = append(subs, cur)
				cur = Path{}
			}
			cur = append(cur, p[i], p[i+1], p[i+2])
			i += 3
		case PathLineTo:
			cur = append(cur, p[i], p[i+1], p[i+2])
			i += 3
		case PathQuadTo:
			cur = append(cur, p[i], p[i+1], p[i+2], p[i+3], p[i+4])
			i += 5
		case PathCubicTo:
			cur = append(cur, p[i], p[i+1], p[i+2], p[i+3], p[i+4], p[i+5], p[i+6])
			i += 7
		case PathClose:
			cur = append(cur, p[i])
			i++
		default:
			// ToSVGPath/AddTo use the same token set, so this is unreachable
			// for paths built through the package.
			i++
		}
	}
	if len(cur) > 0 {
		subs = append(subs, cur)
	}
	return subs
}

// areaCollector is an Adder that accumulates the signed area of the flattened
// path using the shoelace formula. Filler flattens Bezier segments into lines,
// so only Start and Line are exercised. Coordinates arrive as fixed.Int26_6 and
// are converted to pixels; the accumulation runs in float64 to avoid overflow.
type areaCollector struct {
	first, cur fixed.Point26_6
	area       float64
	started    bool
}

func (a *areaCollector) Start(p fixed.Point26_6) {
	a.first, a.cur, a.started = p, p, true
}

func (a *areaCollector) Line(p fixed.Point26_6) {
	if !a.started {
		a.Start(p)
		return
	}
	// Cross product of consecutive edges, in pixel units.
	a.area += (float64(a.cur.X)*float64(p.Y) - float64(p.X)*float64(a.cur.Y)) / (64.0 * 64.0)
	a.cur = p
}

func (a *areaCollector) QuadBezier(b, c fixed.Point26_6) {
	QuadTo(float32(a.cur.X), float32(a.cur.Y), float32(b.X), float32(b.Y),
		float32(c.X), float32(c.Y), func(x, y float32) {
			a.Line(fixed.Point26_6{X: fixed.Int26_6(x), Y: fixed.Int26_6(y)})
		})
}

func (a *areaCollector) CubeBezier(b, c, d fixed.Point26_6) {
	CubeTo(float32(a.cur.X), float32(a.cur.Y), float32(b.X), float32(b.Y),
		float32(c.X), float32(c.Y), float32(d.X), float32(d.Y),
		func(x, y float32) {
			a.Line(fixed.Point26_6{X: fixed.Int26_6(x), Y: fixed.Int26_6(y)})
		})
}

func (a *areaCollector) Stop(closeLoop bool) {
	if closeLoop && a.started && a.cur != a.first {
		a.Line(a.first)
	}
}

// signedArea flattens the subpath and returns its signed area (pixel units
// squared). Coordinates use screen orientation (Y axis points down): a
// visually clockwise contour has positive signed area and its interior lies
// to the port (left) side of travel while its exterior lies to the starboard
// (right) side; a visually counter-clockwise contour has negative signed area
// and the sides are swapped. Reversing a contour flips the sign and swaps the
// sides, so inside and outside strokes are independent of winding direction.
func signedArea(p Path) float64 {
	ac := &areaCollector{}
	p.AddTo(ac)
	return ac.area * 0.5
}

// setSideOffsets assigns the port/starboard offset distances for one closed
// subpath from its signed area. One side always carries the full width and the
// other side zero, so the source contour becomes one boundary of the stroke
// band: the fill region stays the original size on its interior side (outside
// stroke) or on its exterior side (inside stroke).
func (r *Stroker) setSideOffsets(area float64) {
	w := r.width
	interiorIsPort := area > 0
	switch r.strokeMode {
	case StrokeOutside:
		if interiorIsPort {
			// Exterior is starboard.
			r.uPort, r.uStar = 0, w
		} else {
			// Exterior is port.
			r.uPort, r.uStar = w, 0
		}
	case StrokeInside:
		if interiorIsPort {
			r.uPort, r.uStar = w, 0
		} else {
			r.uPort, r.uStar = 0, w
		}
	default: // StrokeCenter; kept for completeness
		r.uPort, r.uStar = r.u, r.u
	}
}

// flushBuffered validates the buffered path, determines the interior side of
// every closed subpath and replays the path through the stroker with the
// resulting per-side offsets. It is invoked from Stop for inside/outside
// placements.
func (r *Stroker) flushBuffered() {
	buf := r.buf
	r.buf = r.buf[:0]
	subs := splitSubpaths(buf)
	r.replaying = true
	defer func() {
		r.replaying = false
		r.inStroke = false
		r.applySideOffsets()
	}()
	for _, sub := range subs {
		// Every subpath must be explicitly closed: an inside/outside stroke
		// has no defined interior side for an open contour.
		if PathCommand(sub[len(sub)-1]) != PathClose {
			panic(ErrOpenPathStroke)
		}
		area := signedArea(sub)
		if math.Abs(area) < zeroAreaTol {
			panic(ErrZeroAreaStroke)
		}
		r.setSideOffsets(area)
		// AddTo drives Start/Line/Bezier/Stop on the receiver; the replay
		// flag routes those calls straight into the stroking machinery.
		sub.AddTo(r)
	}
}
