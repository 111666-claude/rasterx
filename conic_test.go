// Conic gradient tests for the rasterx package
// Copyright 2026 All rights reserved.

package rasterx_test

import (
	"image"
	"image/color"
	"math"
	"testing"

	. "github.com/srwiley/rasterx"
	"golang.org/x/image/colornames"
)

// quadrantStops returns coincident (hard) stops dividing the revolution
// into four solid quarters: red, lime, blue and yellow.
func quadrantStops() []GradStop {
	return []GradStop{
		GradStop{StopColor: colornames.Red, Offset: 0.0, Opacity: 1},
		GradStop{StopColor: colornames.Red, Offset: 0.25, Opacity: 1},
		GradStop{StopColor: colornames.Lime, Offset: 0.25, Opacity: 1},
		GradStop{StopColor: colornames.Lime, Offset: 0.5, Opacity: 1},
		GradStop{StopColor: colornames.Blue, Offset: 0.5, Opacity: 1},
		GradStop{StopColor: colornames.Blue, Offset: 0.75, Opacity: 1},
		GradStop{StopColor: colornames.Yellow, Offset: 0.75, Opacity: 1},
		GradStop{StopColor: colornames.Yellow, Offset: 1.0, Opacity: 1},
	}
}

// wedge returns a pie sector path centered at cx, cy sweeping from
// angle a0 to a1 (radians, y axis down) approximated by line segments.
func wedge(cx, cy, r, a0, a1 float64) (p Path) {
	p.Start(ToFixedP(cx, cy))
	const n = 32
	for i := 0; i <= n; i++ {
		a := a0 + (a1-a0)*float64(i)/n
		p.Line(ToFixedP(cx+r*math.Cos(a), cy+r*math.Sin(a)))
	}
	p.Stop(true)
	return
}

func nrgbaOf(c color.Color) color.NRGBA {
	return color.NRGBAModel.Convert(c).(color.NRGBA)
}

func colorClose(a, b color.NRGBA, tol uint8) bool {
	d := func(x, y uint8) uint8 {
		if x > y {
			return x - y
		}
		return y - x
	}
	return d(a.R, b.R) <= tol && d(a.G, b.G) <= tol &&
		d(a.B, b.B) <= tol && d(a.A, b.A) <= tol
}

// TestConicGradientQuadrants renders a fixed four sector pie chart with a
// conic gradient whose hard stops align with the sector boundaries, then
// checks that each quadrant of the pie shows the expected solid color.
func TestConicGradientQuadrants(t *testing.T) {
	var (
		wx, wy    = 512, 512
		img       = image.NewRGBA(image.Rect(0, 0, wx, wy))
		scannerGV = NewScannerGV(wx, wy, img, img.Bounds())
		d         = NewDasher(wx, wy, scannerGV)
		f         = &d.Filler
	)

	cg := &ConicGradient{Cx: 128, Cy: 128, StartAngle: 0, Clockwise: true,
		Stops: quadrantStops(), Matrix: Identity,
		Spread: RepeatSpread, Units: UserSpaceOnUse}
	scannerGV.SetColor(cg.GetColorFunction(1.0))

	// Fixed sector chart: four 90 degree wedges centered on the gradient.
	for k := 0; k < 4; k++ {
		wedge(128, 128, 100, float64(k)*math.Pi/2, float64(k+1)*math.Pi/2).AddTo(f)
		f.Draw()
		f.Clear()
	}

	// Sample each quadrant at 45 degrees off the axes, radius ~70.
	// With Clockwise set, t sweeps clockwise on screen from the +x axis.
	quadrants := []struct {
		x, y int
		want color.NRGBA
	}{
		{177, 177, color.NRGBA{255, 0, 0, 255}},  // down right: red
		{79, 177, color.NRGBA{0, 255, 0, 255}},   // down left: lime
		{79, 79, color.NRGBA{0, 0, 255, 255}},    // up left: blue
		{177, 79, color.NRGBA{255, 255, 0, 255}}, // up right: yellow
	}
	for _, q := range quadrants {
		got := nrgbaOf(img.At(q.x, q.y))
		if !colorClose(got, q.want, 2) {
			t.Errorf("quadrant at (%d, %d): got %v, want %v", q.x, q.y, got, q.want)
		}
	}

	// A smooth conic gradient in a circle for the visual record.
	cg2 := &ConicGradient{Cx: 384, Cy: 128, StartAngle: math.Pi / 2, Clockwise: true,
		Stops: []GradStop{
			GradStop{StopColor: colornames.Red, Offset: 0.0, Opacity: 1},
			GradStop{StopColor: colornames.Blue, Offset: 0.5, Opacity: 1},
			GradStop{StopColor: colornames.Red, Offset: 1.0, Opacity: 1},
		},
		Matrix: Identity, Spread: RepeatSpread, Units: UserSpaceOnUse}
	scannerGV.SetColor(cg2.GetColorFunction(1.0))
	AddCircle(384, 128, 100, f)
	f.Draw()
	f.Clear()

	// Semi transparent conic gradient in a rect.
	scannerGV.SetColor(cg.GetColorFunction(0.5))
	AddRect(284, 284, 484, 384, 0, f)
	f.Draw()
	f.Clear()

	if err := SaveToPngFile("testdata/conicGV.png", img); err != nil {
		t.Error(err)
	}
}

// TestConicGradientSeam checks that the color is continuous across the
// seam at the start angle when the first and last stops match.
func TestConicGradientSeam(t *testing.T) {
	cg := &ConicGradient{Cx: 128, Cy: 128, StartAngle: math.Pi / 4, Clockwise: true,
		Stops: []GradStop{
			GradStop{StopColor: colornames.Red, Offset: 0.0, Opacity: 1},
			GradStop{StopColor: colornames.Blue, Offset: 0.5, Opacity: 1},
			GradStop{StopColor: colornames.Red, Offset: 1.0, Opacity: 1},
		},
		Matrix: Identity, Spread: RepeatSpread, Units: UserSpaceOnUse}
	cf, ok := cg.GetColorFunction(1.0).(ColorFunc)
	if !ok {
		t.Fatal("expected a ColorFunc")
	}
	// Sample just to either side of the seam ray at 45 degrees.
	const r = 60.0
	a1, a2 := math.Pi/4-0.02, math.Pi/4+0.02
	c1 := nrgbaOf(cf(int(128+r*math.Cos(a1)), int(128+r*math.Sin(a1))))
	c2 := nrgbaOf(cf(int(128+r*math.Cos(a2)), int(128+r*math.Sin(a2))))
	if !colorClose(c1, c2, 8) {
		t.Error("seam not continuous:", c1, c2)
	}
}

// TestConicGradientOpacity checks function opacity and stop opacity handling.
func TestConicGradientOpacity(t *testing.T) {
	cg := &ConicGradient{Cx: 128, Cy: 128, Clockwise: true,
		Stops: []GradStop{
			GradStop{StopColor: colornames.Red, Offset: 0.0, Opacity: 1},
			GradStop{StopColor: colornames.Blue, Offset: 1.0, Opacity: 1},
		},
		Matrix: Identity, Spread: PadSpread, Units: UserSpaceOnUse}
	cf := cg.GetColorFunction(0.5).(ColorFunc)
	if a := nrgbaOf(cf(128, 188)).A; a < 125 || a > 129 {
		t.Error("function opacity not applied, alpha =", a)
	}
	cg.Stops[0].Opacity = 0.5
	cg.Stops[1].Opacity = 0.5
	cf = cg.GetColorFunction(1.0).(ColorFunc)
	if a := nrgbaOf(cf(128, 188)).A; a < 125 || a > 129 {
		t.Error("stop opacity not applied, alpha =", a)
	}
}

// TestConicGradientReflect checks that reflect spread mirrors the stops.
func TestConicGradientReflect(t *testing.T) {
	cg := &ConicGradient{Cx: 128, Cy: 128, Clockwise: true,
		Stops: []GradStop{
			GradStop{StopColor: colornames.Red, Offset: 0.0, Opacity: 1},
			GradStop{StopColor: colornames.Blue, Offset: 1.0, Opacity: 1},
		},
		Matrix: Identity, Spread: ReflectSpread, Units: UserSpaceOnUse}
	cf := cg.GetColorFunction(1.0).(ColorFunc)
	// t = 0.25 straight down and t = -0.25 straight up must match.
	c1 := nrgbaOf(cf(128, 188))
	c2 := nrgbaOf(cf(128, 68))
	if c1 != c2 {
		t.Error("reflect spread not symmetric:", c1, c2)
	}
}

// TestConicGradientUnitsMatrix checks that object bounding box and user
// space gradients agree, and that the gradient matrix is honored.
func TestConicGradientUnitsMatrix(t *testing.T) {
	samples := [][2]int{{177, 177}, {79, 100}, {128, 188}, {100, 60}}

	obb := &ConicGradient{Cx: 0.5, Cy: 0.5, Clockwise: true,
		Stops: quadrantStops(), Matrix: Identity, Spread: RepeatSpread,
		Units:  ObjectBoundingBox,
		Bounds: struct{ X, Y, W, H float64 }{X: 64, Y: 64, W: 128, H: 128}}
	us := &ConicGradient{Cx: 128, Cy: 128, Clockwise: true,
		Stops: quadrantStops(), Matrix: Identity, Spread: RepeatSpread,
		Units: UserSpaceOnUse}
	cfOBB := obb.GetColorFunction(1.0).(ColorFunc)
	cfUS := us.GetColorFunction(1.0).(ColorFunc)
	for _, s := range samples {
		if c1, c2 := nrgbaOf(cfOBB(s[0], s[1])), nrgbaOf(cfUS(s[0], s[1])); c1 != c2 {
			t.Errorf("OBB vs user space mismatch at %v: %v != %v", s, c1, c2)
		}
	}

	// A translated gradient matrix must shift the gradient center.
	shifted := &ConicGradient{Cx: 118, Cy: 108, Clockwise: true,
		Stops: quadrantStops(), Matrix: Identity.Translate(10, 20),
		Spread: RepeatSpread, Units: UserSpaceOnUse}
	cfShift := shifted.GetColorFunction(1.0).(ColorFunc)
	for _, s := range samples {
		if c1, c2 := nrgbaOf(cfShift(s[0], s[1])), nrgbaOf(cfUS(s[0], s[1])); c1 != c2 {
			t.Errorf("matrix shift mismatch at %v: %v != %v", s, c1, c2)
		}
	}

	// GetColorFunctionUS must apply the user space object matrix as well.
	usObj := &ConicGradient{Cx: 108, Cy: 108, Clockwise: true,
		Stops: quadrantStops(), Matrix: Identity,
		Spread: RepeatSpread, Units: UserSpaceOnUse}
	cfObj := usObj.GetColorFunctionUS(1.0, Identity.Translate(20, 20)).(ColorFunc)
	for _, s := range samples {
		if c1, c2 := nrgbaOf(cfObj(s[0], s[1])), nrgbaOf(cfUS(s[0], s[1])); c1 != c2 {
			t.Errorf("objMatrix shift mismatch at %v: %v != %v", s, c1, c2)
		}
	}
}

// TestConicGradientEdgeCases checks degenerate inputs are handled explicitly.
func TestConicGradientEdgeCases(t *testing.T) {
	errColor := color.NRGBA{0, 0, 0, 255}

	// Empty stops give the default error color.
	empty := &ConicGradient{Cx: 10, Cy: 10, Matrix: Identity}
	if got := nrgbaOf(empty.GetColorFunction(1.0).(color.Color)); got != errColor {
		t.Error("empty stops: got", got, "want", errColor)
	}

	// A single stop gives that stop's color with its opacity.
	single := &ConicGradient{Cx: 10, Cy: 10, Matrix: Identity,
		Stops: []GradStop{GradStop{StopColor: colornames.Red, Offset: 0.3, Opacity: 0.5}}}
	if got := nrgbaOf(single.GetColorFunction(1.0).(color.Color)); got != (color.NRGBA{255, 0, 0, 127}) {
		t.Error("single stop: got", got)
	}

	// Non-finite centers give the error color.
	for _, c := range [][2]float64{{math.NaN(), 0}, {0, math.NaN()}, {math.Inf(1), 0}, {0, math.Inf(-1)}} {
		bad := &ConicGradient{Cx: c[0], Cy: c[1], Stops: quadrantStops(), Matrix: Identity}
		if got := nrgbaOf(bad.GetColorFunction(1.0).(color.Color)); got != errColor {
			t.Errorf("center %v: got %v, want %v", c, got, errColor)
		}
	}

	// Zero size bounds give the error color instead of NaN colors.
	zero := &ConicGradient{Cx: 0.5, Cy: 0.5, Stops: quadrantStops(), Matrix: Identity,
		Units:  ObjectBoundingBox,
		Bounds: struct{ X, Y, W, H float64 }{X: 0, Y: 0, W: 0, H: 100}}
	if got := nrgbaOf(zero.GetColorFunction(1.0).(color.Color)); got != errColor {
		t.Error("zero width bounds: got", got, "want", errColor)
	}

	// A non-finite start angle is treated as zero.
	nanStart := &ConicGradient{Cx: 128, Cy: 128, StartAngle: math.NaN(), Clockwise: true,
		Stops: quadrantStops(), Matrix: Identity, Spread: RepeatSpread, Units: UserSpaceOnUse}
	zeroStart := &ConicGradient{Cx: 128, Cy: 128, StartAngle: 0, Clockwise: true,
		Stops: quadrantStops(), Matrix: Identity, Spread: RepeatSpread, Units: UserSpaceOnUse}
	cfNaN := nanStart.GetColorFunction(1.0).(ColorFunc)
	cfZero := zeroStart.GetColorFunction(1.0).(ColorFunc)
	if c1, c2 := nrgbaOf(cfNaN(177, 100)), nrgbaOf(cfZero(177, 100)); c1 != c2 {
		t.Error("NaN start angle not treated as zero:", c1, c2)
	}

	// Extremely large coordinates must not panic or produce a NaN color.
	huge := &ConicGradient{Cx: 128, Cy: 128, Clockwise: true,
		Stops: quadrantStops(), Matrix: Identity, Spread: RepeatSpread, Units: UserSpaceOnUse}
	cfHuge := huge.GetColorFunction(1.0).(ColorFunc)
	if got := nrgbaOf(cfHuge(1<<40, -(1 << 40))); got.A != 255 {
		t.Error("huge coordinates: got", got)
	}
}
