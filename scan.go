// Package rasterx implements a rasterizer in go.
// By default rasterx uses ScannerGV to render images
// which uses the rasterizer in the golang.org/x/image/vector package.
// The freetype rasterizer under the GNU license can also be used, by
// downloading the scanFT package.
//
// Copyright 2018 All rights reserved.
// Created: 5/12/2018 by S.R.Wiley
package rasterx

import (
	"image"
	"math"

	"image/color"
	"image/draw"

	"golang.org/x/image/math/fixed"
	"golang.org/x/image/vector"
)

// At returns the color at the point x,y
func (c *ColorFuncImage) At(x, y int) color.Color {
	return c.colorFunc(x, y)
}

type (
	// ColorFuncImage implements and image
	// using the provided colorFunc
	ColorFuncImage struct {
		image.Uniform
		colorFunc ColorFunc
	}

	// ScannerGV uses the google vector rasterizer
	ScannerGV struct {
		r vector.Rasterizer
		//a, first fixed.Point26_6
		Dest                   draw.Image
		Targ                   image.Rectangle
		clipImage              *ClipImage
		Source                 image.Image
		Offset                 image.Point
		minX, minY, maxX, maxY fixed.Int26_6 // keep track of bounds
		colorIsFunc            bool          // remembers the last SetColor variant
	}
)

// ClipImage is a clipable ColorFuncImage
type ClipImage struct {
	ColorFuncImage
	clip image.Rectangle
	// mask is an optional path clip mask; when non-nil the source color
	// alpha is scaled by the mask coverage at each pixel. maskOff is the
	// offset between source coordinates and mask coordinates (the value of
	// the scanner's Offset when the mask was created).
	mask    *image.Alpha
	maskOff image.Point
}

var noApha = color.RGBA{0, 0, 0, 0}

// GetPathExtent returns the extent of the path
func (s *ScannerGV) GetPathExtent() fixed.Rectangle26_6 {
	return fixed.Rectangle26_6{Min: fixed.Point26_6{X: s.minX, Y: s.minY}, Max: fixed.Point26_6{X: s.maxX, Y: s.maxY}}
}

// At returns the color of the ClipImage at the point x,y
func (c *ClipImage) At(x, y int) color.Color {
	if c.clip != image.ZR && !(image.Point{x, y}).In(c.clip) {
		return noApha
	}
	col := c.ColorFuncImage.At(x, y)
	if c.mask == nil {
		return col
	}
	// A path clip mask is set: scale the source coverage by the mask
	// coverage so anti-aliased clip edges composite correctly.
	mx, my := x-c.maskOff.X, y-c.maskOff.Y
	if !(image.Point{mx, my}).In(c.mask.Bounds()) {
		return noApha
	}
	m := uint32(c.mask.AlphaAt(mx, my).A)
	switch m {
	case 0:
		return noApha
	case 255:
		return col
	}
	// col.RGBA() returns alpha-premultiplied values, so scaling all four
	// channels by the mask coverage yields the correctly composited color.
	r, g, b, a := col.RGBA()
	return color.RGBA64{
		R: uint16(r * m / 255),
		G: uint16(g * m / 255),
		B: uint16(b * m / 255),
		A: uint16(a * m / 255),
	}
}

// SetWinding set the winding rule for the scanner
func (s *ScannerGV) SetWinding(useNonZeroWinding bool) {
	// no-op as scanner gv does not support even-odd winding
}

// SetColor set the color type for the scanner
func (s *ScannerGV) SetColor(clr interface{}) {
	clipped := s.clipImage.clip != image.ZR || s.clipImage.mask != nil
	switch c := clr.(type) {
	case color.Color:
		s.colorIsFunc = false
		s.clipImage.ColorFuncImage.Uniform.C = c
		if !clipped {
			s.Source = &s.clipImage.ColorFuncImage.Uniform
		} else {
			s.clipImage.ColorFuncImage.colorFunc = func(x, y int) color.Color {
				return c
			}
			s.Source = s.clipImage
		}
	case ColorFunc:
		s.colorIsFunc = true
		s.clipImage.ColorFuncImage.colorFunc = c
		if !clipped {
			s.Source = &s.clipImage.ColorFuncImage
		} else {
			s.Source = s.clipImage
		}
	}
}

// refreshSource re-applies the current color so that Source reflects the
// current clipping state (rectangle clip and/or path clip mask).
func (s *ScannerGV) refreshSource() {
	if s.colorIsFunc {
		s.SetColor(s.clipImage.ColorFuncImage.colorFunc)
	} else {
		s.SetColor(s.clipImage.ColorFuncImage.Uniform.C)
	}
}

// SetClip sets an optional clipping rectangle to restrict rendering only to
// that region -- if size is 0 then ignored (set to image.ZR to clear)
func (s *ScannerGV) SetClip(rect image.Rectangle) {
	s.clipImage.clip = rect
	s.refreshSource()
}

// SetClipPath sets an optional clipping path; only the region covered by the
// path (non-zero winding) is rendered, intersected with any clipping
// rectangle set by SetClip. The path is rasterized once into an anti-aliased
// coverage mask in the scanner's coordinate space, so the same mask is
// shared by all subsequent Draw calls; the path itself is not modified and
// may be reused by the caller. Mask edges are anti-aliased: coverage at the
// boundary scales the source color alpha. Passing a nil or empty path is
// equivalent to ClearClipPath. A path with no filled area (e.g. only MoveTo
// commands) clips away everything. The mask uses one byte per pixel of the
// scanner bounds, and the buffer is reused between SetClipPath calls of the
// same size to limit allocation churn on large canvases.
func (s *ScannerGV) SetClipPath(p Path) {
	if len(p) == 0 {
		s.ClearClipPath()
		return
	}
	size := s.r.Size()
	w, h := size.X, size.Y
	mask := s.clipImage.mask
	if mask == nil || mask.Bounds().Dx() != w || mask.Bounds().Dy() != h {
		if w < 0 {
			w = 0
		}
		if h < 0 {
			h = 0
		}
		mask = image.NewAlpha(image.Rect(0, 0, w, h))
	} else {
		clearMask(mask)
	}
	var z vector.Rasterizer
	z.Reset(w, h)
	addPathToRasterizer(&z, p)
	z.Draw(mask, mask.Bounds(), image.Opaque, image.Point{})
	s.clipImage.mask = mask
	s.clipImage.maskOff = s.Offset
	s.refreshSource()
}

// ClearClipPath removes any clipping path set by SetClipPath; rendering is
// then restricted only by the clipping rectangle, if any.
func (s *ScannerGV) ClearClipPath() {
	if s.clipImage.mask == nil {
		return
	}
	s.clipImage.mask = nil
	s.refreshSource()
}

// clearMask zeros an alpha mask so it can be reused.
func clearMask(m *image.Alpha) {
	for i := range m.Pix {
		m.Pix[i] = 0
	}
}

// addPathToRasterizer replays the commands of p into z without modifying p.
// Open subpaths are implicitly closed, as in SVG fill semantics, so that
// degenerate subpaths (e.g. a lone MoveTo/LineTo) have zero area and clip
// away everything instead of relying on undefined rasterizer behavior.
func addPathToRasterizer(z *vector.Rasterizer, p Path) {
	started := false
	for i := 0; i < len(p); {
		switch PathCommand(p[i]) {
		case PathMoveTo:
			if started {
				z.ClosePath()
			}
			z.MoveTo(float32(p[i+1])/64, float32(p[i+2])/64)
			started = true
			i += 3
		case PathLineTo:
			z.LineTo(float32(p[i+1])/64, float32(p[i+2])/64)
			i += 3
		case PathQuadTo:
			z.QuadTo(float32(p[i+1])/64, float32(p[i+2])/64,
				float32(p[i+3])/64, float32(p[i+4])/64)
			i += 5
		case PathCubicTo:
			z.CubeTo(float32(p[i+1])/64, float32(p[i+2])/64,
				float32(p[i+3])/64, float32(p[i+4])/64,
				float32(p[i+5])/64, float32(p[i+6])/64)
			i += 7
		case PathClose:
			z.ClosePath()
			started = false
			i++
		default:
			panic("rasterx: SetClipPath: bad path")
		}
	}
	if started {
		z.ClosePath()
	}
}

func (s *ScannerGV) set(a fixed.Point26_6) {
	if s.maxX < a.X {
		s.maxX = a.X
	}
	if s.maxY < a.Y {
		s.maxY = a.Y
	}
	if s.minX > a.X {
		s.minX = a.X
	}
	if s.minY > a.Y {
		s.minY = a.Y
	}
}

// Start starts a new path at the given point.
func (s *ScannerGV) Start(a fixed.Point26_6) {
	s.set(a)
	s.r.MoveTo(float32(a.X)/64, float32(a.Y)/64)
}

// Line adds a linear segment to the current curve.
func (s *ScannerGV) Line(b fixed.Point26_6) {
	s.set(b)
	s.r.LineTo(float32(b.X)/64, float32(b.Y)/64)
}

// Draw renders the accumulate scan to the desination
func (s *ScannerGV) Draw() {
	// This draws the entire bounds of the image, because
	// at this point the alpha mask does not shift with the
	// placement of the target rectangle in the vector rasterizer
	s.r.Draw(s.Dest, s.Dest.Bounds(), s.Source, s.Offset)

	// Remove the line above and uncomment the lines below if you
	// are using a version of the vector rasterizer that shifts the alpha
	// mask with the placement of the target

	//	s.Targ.Min.X = int(s.minX >> 6)
	//	s.Targ.Min.Y = int(s.minY >> 6)
	//	s.Targ.Max.X = int(s.maxX>>6) + 1
	//	s.Targ.Max.Y = int(s.maxY>>6) + 1
	//	s.Targ = s.Targ.Intersect(s.Dest.Bounds())  // This check should be done by the rasterizer?
	//	s.r.Draw(s.Dest, s.Targ, s.Source, s.Offset)
}

// Clear cancels any previous accumulated scans
func (s *ScannerGV) Clear() {
	p := s.r.Size()
	s.r.Reset(p.X, p.Y)
	const mxfi = fixed.Int26_6(math.MaxInt32)
	s.minX, s.minY, s.maxX, s.maxY = mxfi, mxfi, -mxfi, -mxfi
}

// SetBounds sets the maximum width and height of the rasterized image and
// calls Clear. The width and height are in pixels, not fixed.Int26_6 units.
// If the size changes, any clipping path is removed, since its mask no
// longer matches the bounds; call SetClipPath again after resizing.
func (s *ScannerGV) SetBounds(width, height int) {
	sizeChanged := s.r.Size() != image.Pt(width, height)
	s.r.Reset(width, height)
	if sizeChanged && s.clipImage != nil && s.clipImage.mask != nil {
		s.clipImage.mask = nil
		s.refreshSource()
	}
}

// NewScannerGV creates a new Scanner with the given bounds.
func NewScannerGV(width, height int, dest draw.Image,
	targ image.Rectangle) *ScannerGV {
	s := new(ScannerGV)
	s.SetBounds(width, height)
	s.Dest = dest
	s.Targ = targ
	s.clipImage = &ClipImage{}
	s.clipImage.ColorFuncImage.Uniform.C = &color.RGBA{255, 0, 0, 255}
	s.Source = &s.clipImage.ColorFuncImage.Uniform
	s.Offset = image.Point{0, 0}
	return s
}
