// Copyright 2026 by the rasterx Authors. All rights reserved.
//
// Non-scaling stroke support (analogous to the SVG
// vector-effect="non-scaling-stroke"). When enabled, the affine
// transformation is applied to the path geometry first and the stroke is
// then built entirely in device space, so the stroke width, caps, joins
// and dash lengths stay constant on screen regardless of any
// (including non-uniform) scaling, rotation or shearing applied to the
// path.

package rasterx

import (
	"math"

	"golang.org/x/image/math/fixed"
)

// safeFixedLimit is the largest fixed.Int26_6 magnitude fed to the
// rasterizer. It equals 2^24 pixels expressed in 1/64 units (2^24 * 64 =
// 2^30). Beyond 2^24 pixels, float32 (the rasterizer's working precision,
// with a 24-bit mantissa) can no longer resolve individual pixel steps, so
// restricting coordinates to this range keeps every segment representable.
// The limit is far beyond any real canvas, and geometry outside the image
// bounds is clipped regardless, so clamping cannot change visible output.
const safeFixedLimit = 1 << 30

// clampFixed rounds f to a fixed.Int26_6 value, mapping non-finite values
// (NaN, +-Inf produced by singular or otherwise ill-conditioned matrices)
// to zero and saturating values outside the float32-safe range. This gives
// extreme coordinates and singular matrices a deterministic result instead
// of implementation-defined float-to-int behavior or an upstream rasterizer
// division by zero.
func clampFixed(f float64) fixed.Int26_6 {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0
	}
	if f >= safeFixedLimit {
		return fixed.Int26_6(safeFixedLimit)
	}
	if f <= -safeFixedLimit {
		return fixed.Int26_6(-safeFixedLimit)
	}
	return fixed.Int26_6(f)
}

// tFixedSat transforms the fixed point p by m in the same coordinate units
// as Matrix2D.TFixed, but guards against non-finite and overflowing results.
// A singular matrix therefore never panics: collinear directions collapse
// deterministically and any undefined component becomes zero.
func (m Matrix2D) tFixedSat(p fixed.Point26_6) fixed.Point26_6 {
	px, py := float64(p.X), float64(p.Y)
	return fixed.Point26_6{
		X: clampFixed(px*m.A + py*m.C + m.E*64),
		Y: clampFixed(px*m.B + py*m.D + m.F*64),
	}
}

// mapStrokePoint applies the non-scaling transformation to p when non-scaling
// mode is active. In normal mode it returns p unchanged so the existing
// stroking behavior is bit-for-bit preserved.
func (r *Stroker) mapStrokePoint(p fixed.Point26_6) fixed.Point26_6 {
	if !r.NonScaling {
		return p
	}
	return r.Transformer.tFixedSat(p)
}
