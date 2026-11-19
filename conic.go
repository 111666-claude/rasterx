// Conic gradient implementation for the rasterx package
// Copyright 2026 All rights reserved.

package rasterx

import (
	"image/color"
	"math"
	"sort"
)

type (
	// ConicGradient holds a description of a conic (angular) gradient, in
	// which the color varies with the angle around a center point, as used
	// for circular gauges and pie charts. The gradient parameter t is 0 at
	// StartAngle and sweeps through one full revolution to 1, reusing the
	// same stops, spread methods and opacity handling as the linear and
	// radial Gradient type.
	//
	// Angles are measured in radians from the positive x axis, positive
	// toward the positive y axis. Since the y axis of the raster image
	// points downward, increasing angles sweep clockwise on screen when
	// Clockwise is true; if Clockwise is false the sweep direction is
	// reversed. For ObjectBoundingBox units Cx and Cy are fractions of the
	// bounds, otherwise they are user space coordinates.
	ConicGradient struct {
		Cx, Cy     float64 // Center of the gradient
		StartAngle float64 // Angle in radians at which t is 0
		Clockwise  bool    // If true t sweeps clockwise on screen
		Stops      []GradStop
		Bounds     struct{ X, Y, W, H float64 }
		Matrix     Matrix2D
		Spread     SpreadMethod
		Units      GradientUnits
	}
)

// conicErrorColor is the default color returned for degenerate conic
// gradient descriptions, matching the error color used by Gradient
// for gradients without stops.
func conicErrorColor(opacity float64) color.Color {
	return ApplyOpacity(color.RGBA{0, 0, 0, 255}, opacity)
}

func isFiniteF(x float64) bool {
	return !math.IsNaN(x) && !math.IsInf(x, 0)
}

// GetColorFunction returns the color function for the conic gradient.
func (c *ConicGradient) GetColorFunction(opacity float64) interface{} {
	return c.GetColorFunctionUS(opacity, Identity)
}

// GetColorFunctionUS returns the color function using the User Space objMatrix
func (c *ConicGradient) GetColorFunctionUS(opacity float64, objMatrix Matrix2D) interface{} {
	switch len(c.Stops) {
	case 0:
		return conicErrorColor(opacity) // default error color for gradient w/o stops.
	case 1:
		s := c.Stops[0]
		return ApplyOpacity(s.StopColor, s.Opacity*opacity)
	}
	if !isFiniteF(c.Cx) || !isFiniteF(c.Cy) {
		return conicErrorColor(opacity) // invalid center
	}
	startAngle := c.StartAngle
	if !isFiniteF(startAngle) {
		startAngle = 0 // non-finite start angles are treated as zero
	}

	// sort by offset in ascending order
	sort.Slice(c.Stops, func(i, j int) bool {
		return c.Stops[i].Offset < c.Stops[j].Offset
	})

	// Reuse the Gradient stop, spread and opacity logic so that coincident
	// stops, pad/reflect/repeat spreads and opacity behave exactly as they
	// do for linear and radial gradients.
	g := &Gradient{Stops: c.Stops, Spread: c.Spread}

	tColor := func(dx, dy float64) color.Color {
		// The y axis points down, so ang grows clockwise on screen.
		ang := math.Atan2(dy, dx)
		if math.IsNaN(ang) { // only possible for non-finite dx or dy
			return conicErrorColor(opacity)
		}
		rel := ang - startAngle
		if !c.Clockwise {
			rel = -rel
		}
		return g.tColor(rel/(2*math.Pi), opacity)
	}

	if c.Units == ObjectBoundingBox {
		w, h := c.Bounds.W, c.Bounds.H
		oriX, oriY := c.Bounds.X, c.Bounds.Y
		if !(w > 0) || !(h > 0) || !isFiniteF(w) || !isFiniteF(h) ||
			!isFiniteF(oriX) || !isFiniteF(oriY) {
			return conicErrorColor(opacity) // zero size or invalid bounds
		}
		gradT := Identity.Translate(oriX, oriY).Scale(w, h).
			Mult(c.Matrix).Scale(1/w, 1/h).Translate(-oriX, -oriY).Invert()
		cx := c.Bounds.X + w*c.Cx
		cy := c.Bounds.Y + h*c.Cy
		return ColorFunc(func(xi, yi int) color.Color {
			x, y := gradT.Transform(float64(xi)+0.5, float64(yi)+0.5)
			return tColor(x-cx, y-cy)
		})
	}

	cx, cy := c.Matrix.Transform(c.Cx, c.Cy)
	cx, cy = objMatrix.Transform(cx, cy)
	return ColorFunc(func(xi, yi int) color.Color {
		x := float64(xi) + 0.5
		y := float64(yi) + 0.5
		return tColor(x-cx, y-cy)
	})
}
