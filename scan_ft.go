// Copyright 2010 The Freetype-Go Authors. All rights reserved.
// Use of this source code is governed by your choice of either the
// FreeType License or the GNU General Public License version 2 (or
// any later version), both of which can be found in the LICENSE file.
//
// 2017: This is the anti-aliasing algorithm from the golang translation
// of FreeType. It has been adapted for use by the rasterx package, which
// can use either this scanner or ScannerGV (based on the
// golang.org/x/image/vector package). Unlike ScannerGV, this scanner
// supports both the non-zero and the even-odd (parity) winding rule.
//
// Rasterization is done by the same area/coverage accumulation algorithm as
// the Freetype "smooth" module, and the Anti-Grain Geometry library. A
// description of the area/coverage algorithm is at
// http://projects.tuxee.net/cl-vectors/section-the-cl-aa-algorithm

package rasterx

import (
	"image"
	"image/color"
	"image/draw"
	"math"

	"golang.org/x/image/math/fixed"
)

type (
	// ftCell is part of a linked list (for a given yi coordinate) of accumulated
	// area/coverage for the pixel at (xi, yi).
	ftCell struct {
		xi          int
		area, cover int
		next        int
	}

	// ScannerFT is an anti-aliasing 2-D rasterizer using the area/coverage
	// accumulation algorithm. It satisfies the Scanner interface and, unlike
	// ScannerGV, supports the even-odd winding rule through SetWinding.
	//
	// SetWinding(true) (the default) selects the non-zero winding rule and
	// SetWinding(false) selects the even-odd winding rule. The choice only
	// affects the Draw step: the accumulated area/coverage values are the same
	// for both rules, so the rule can be switched at any time before Draw
	// without re-feeding the path.
	ScannerFT struct {
		// If false, the behavior is to use the even-odd winding fill
		// rule during Draw.
		UseNonZeroWinding bool

		// The width of the rasterizer. The height is implicit in
		// len(cellIndex).
		width int

		// The current pen position.
		a fixed.Point26_6
		// The current cell and its area/coverage being accumulated.
		xi, yi      int
		area, cover int

		// Saved cells.
		cell []ftCell
		// Linked list of cells, one per row.
		cellIndex []int
		// Small stack buffers to avoid heap allocation for simple paths.
		cellBuf      [256]ftCell
		cellIndexBuf [64]int
		spanBuf      [64]ftSpan

		Dest draw.Image
		Targ image.Rectangle
		// clipImage restricts painting to its clip rectangle while still
		// evaluating the color function at every painted pixel.
		clipImage *ClipImage
		Source    image.Image
		Offset    image.Point
		clip      image.Rectangle

		minX, minY, maxX, maxY fixed.Int26_6 // keep track of bounds
	}

	// ftSpan is a horizontal segment of pixels with constant alpha. X0 is an
	// inclusive bound and X1 is exclusive, the same as for slices. A fully
	// opaque span has alpha == 0xffff.
	ftSpan struct {
		Y, X0, X1 int
		Alpha     uint32
	}
)

// GetPathExtent returns the extent of the accumulated path
func (s *ScannerFT) GetPathExtent() fixed.Rectangle26_6 {
	return fixed.Rectangle26_6{
		Min: fixed.Point26_6{X: s.minX, Y: s.minY},
		Max: fixed.Point26_6{X: s.maxX, Y: s.maxY}}
}

// SetWinding sets the winding rule for the scanner. Pass true for the
// non-zero winding rule (the default) or false for the even-odd winding
// rule.
func (s *ScannerFT) SetWinding(useNonZeroWinding bool) {
	s.UseNonZeroWinding = useNonZeroWinding
}

// SetColor sets the color type for the scanner. It accepts either a
// color.Color or a ColorFunc, mirroring ScannerGV.SetColor.
func (s *ScannerFT) SetColor(clr interface{}) {
	switch c := clr.(type) {
	case color.Color:
		s.clipImage.ColorFuncImage.Uniform.C = c
		if s.clipImage.clip == image.ZR {
			s.Source = &s.clipImage.ColorFuncImage.Uniform
		} else {
			s.clipImage.ColorFuncImage.colorFunc = func(x, y int) color.Color {
				return c
			}
			s.Source = s.clipImage
		}
	case ColorFunc:
		s.clipImage.ColorFuncImage.colorFunc = c
		if s.clipImage.clip == image.ZR {
			s.Source = &s.clipImage.ColorFuncImage
		} else {
			s.Source = s.clipImage
		}
	}
}

// SetClip sets an optional clipping rectangle to restrict rendering only to
// that region -- if size is 0 then ignored (set to image.ZR to clear)
func (s *ScannerFT) SetClip(rect image.Rectangle) {
	s.clipImage.clip = rect
	if s.Source == &s.clipImage.ColorFuncImage.Uniform {
		s.SetColor(s.clipImage.ColorFuncImage.Uniform.C)
	} else {
		s.SetColor(s.clipImage.ColorFuncImage.colorFunc)
	}
}

func (s *ScannerFT) set(a fixed.Point26_6) {
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

// findCell returns the index in s.cell for the cell corresponding to
// (s.xi, s.yi). The cell is created if necessary.
func (s *ScannerFT) findCell() int {
	if s.yi < 0 || s.yi >= len(s.cellIndex) {
		return -1
	}
	xi := s.xi
	if xi < 0 {
		xi = -1
	} else if xi > s.width {
		xi = s.width
	}
	i, prev := s.cellIndex[s.yi], -1
	for i != -1 && s.cell[i].xi <= xi {
		if s.cell[i].xi == xi {
			return i
		}
		i, prev = s.cell[i].next, i
	}
	c := len(s.cell)
	if c == cap(s.cell) {
		buf := make([]ftCell, c, 4*c)
		copy(buf, s.cell)
		s.cell = buf[0 : c+1]
	} else {
		s.cell = s.cell[0 : c+1]
	}
	s.cell[c] = ftCell{xi, 0, 0, i}
	if prev == -1 {
		s.cellIndex[s.yi] = c
	} else {
		s.cell[prev].next = c
	}
	return c
}

// saveCell saves any accumulated s.area/s.cover for (s.xi, s.yi).
func (s *ScannerFT) saveCell() {
	if s.area != 0 || s.cover != 0 {
		i := s.findCell()
		if i != -1 {
			s.cell[i].area += s.area
			s.cell[i].cover += s.cover
		}
		s.area = 0
		s.cover = 0
	}
}

// setCell sets the (xi, yi) cell that s is accumulating area/coverage for.
func (s *ScannerFT) setCell(xi, yi int) {
	if s.xi != xi || s.yi != yi {
		s.saveCell()
		s.xi, s.yi = xi, yi
	}
}

// scan accumulates area/coverage for the yi'th scanline, going from
// x0 to x1 in the horizontal direction (in 26.6 fixed point coordinates)
// and from y0f to y1f fractional vertical units within that scanline.
func (s *ScannerFT) scan(yi int, x0, y0f, x1, y1f fixed.Int26_6) {
	// Break the 26.6 fixed point X coordinates into integral and fractional parts.
	x0i := int(x0) / 64
	x0f := x0 - fixed.Int26_6(64*x0i)
	x1i := int(x1) / 64
	x1f := x1 - fixed.Int26_6(64*x1i)

	// A perfectly horizontal scan.
	if y0f == y1f {
		s.setCell(x1i, yi)
		return
	}
	dx, dy := x1-x0, y1f-y0f
	// A single cell scan.
	if x0i == x1i {
		s.area += int((x0f + x1f) * dy)
		s.cover += int(dy)
		return
	}
	// There are at least two cells. Apart from the first and last cells,
	// all intermediate cells go through the full width of the cell,
	// or 64 units in 26.6 fixed point format.
	var (
		p, q, edge0, edge1 fixed.Int26_6
		xiDelta            int
	)
	if dx > 0 {
		p, q = (64-x0f)*dy, dx
		edge0, edge1, xiDelta = 0, 64, 1
	} else {
		p, q = x0f*dy, -dx
		edge0, edge1, xiDelta = 64, 0, -1
	}
	yDelta, yRem := p/q, p%q
	if yRem < 0 {
		yDelta--
		yRem += q
	}
	// Do the first cell.
	xi, y := x0i, y0f
	s.area += int((x0f + edge1) * yDelta)
	s.cover += int(yDelta)
	xi, y = xi+xiDelta, y+yDelta
	s.setCell(xi, yi)
	if xi != x1i {
		// Do all the intermediate cells.
		p = 64 * (y1f - y + yDelta)
		fullDelta, fullRem := p/q, p%q
		if fullRem < 0 {
			fullDelta--
			fullRem += q
		}
		yRem -= q
		for xi != x1i {
			yDelta = fullDelta
			yRem += fullRem
			if yRem >= 0 {
				yDelta++
				yRem -= q
			}
			s.area += int(64 * yDelta)
			s.cover += int(yDelta)
			xi, y = xi+xiDelta, y+yDelta
			s.setCell(xi, yi)
		}
	}
	// Do the last cell.
	yDelta = y1f - y
	s.area += int((edge0 + x1f) * yDelta)
	s.cover += int(yDelta)
}

// Start starts a new path at the given point.
func (s *ScannerFT) Start(a fixed.Point26_6) {
	s.set(a)
	s.setCell(int(a.X/64), int(a.Y/64))
	s.a = a
}

// Line adds a linear segment to the current curve.
func (s *ScannerFT) Line(b fixed.Point26_6) {
	s.set(b)
	x0, y0 := s.a.X, s.a.Y
	x1, y1 := b.X, b.Y
	dx, dy := x1-x0, y1-y0
	// Break the 26.6 fixed point Y coordinates into integral and fractional parts.
	y0i := int(y0) / 64
	y0f := y0 - fixed.Int26_6(64*y0i)
	y1i := int(y1) / 64
	y1f := y1 - fixed.Int26_6(64*y1i)

	if y0i == y1i {
		// There is only one scanline.
		s.scan(y0i, x0, y0f, x1, y1f)

	} else if dx == 0 {
		// This is a vertical line segment. We avoid calling s.scan and instead
		// manipulate s.area and s.cover directly.
		var (
			edge0, edge1 fixed.Int26_6
			yiDelta      int
		)
		if dy > 0 {
			edge0, edge1, yiDelta = 0, 64, 1
		} else {
			edge0, edge1, yiDelta = 64, 0, -1
		}
		x0i, yi := int(x0)/64, y0i
		x0fTimes2 := (int(x0) - (64 * x0i)) * 2
		// Do the first pixel.
		dcover := int(edge1 - y0f)
		darea := int(x0fTimes2 * dcover)
		s.area += darea
		s.cover += dcover
		yi += yiDelta
		s.setCell(x0i, yi)
		// Do all the intermediate pixels.
		dcover = int(edge1 - edge0)
		darea = int(x0fTimes2 * dcover)
		for yi != y1i {
			s.area += darea
			s.cover += dcover
			yi += yiDelta
			s.setCell(x0i, yi)
		}
		// Do the last pixel.
		dcover = int(y1f - edge0)
		darea = int(x0fTimes2 * dcover)
		s.area += darea
		s.cover += dcover

	} else {
		// There are at least two scanlines. Apart from the first and last
		// scanlines, all intermediate scanlines go through the full height of
		// the row, or 64 units in 26.6 fixed point format.
		var (
			p, q, edge0, edge1 fixed.Int26_6
			yiDelta            int
		)
		if dy > 0 {
			p, q = (64-y0f)*dx, dy
			edge0, edge1, yiDelta = 0, 64, 1
		} else {
			p, q = y0f*dx, -dy
			edge0, edge1, yiDelta = 64, 0, -1
		}
		xDelta, xRem := p/q, p%q
		if xRem < 0 {
			xDelta--
			xRem += q
		}
		// Do the first scanline.
		x, yi := x0, y0i
		s.scan(yi, x, y0f, x+xDelta, edge1)
		x, yi = x+xDelta, yi+yiDelta
		s.setCell(int(x)/64, yi)
		if yi != y1i {
			// Do all the intermediate scanlines.
			p = 64 * dx
			fullDelta, fullRem := p/q, p%q
			if fullRem < 0 {
				fullDelta--
				fullRem += q
			}
			xRem -= q
			for yi != y1i {
				xDelta = fullDelta
				xRem += fullRem
				if xRem >= 0 {
					xDelta++
					xRem -= q
				}
				s.scan(yi, x, edge0, x+xDelta, edge1)
				x, yi = x+xDelta, yi+yiDelta
				s.setCell(int(x)/64, yi)
			}
		}
		// Do the last scanline.
		s.scan(yi, x, edge0, x1, y1f)
	}
	// The next Line starts from b.
	s.a = b
}

// areaToAlpha converts an area value to a uint32 alpha value. A completely
// filled pixel corresponds to an area of 64*64*2, and an alpha of 0xffff. The
// conversion of area values greater than this depends on the winding rule:
// even-odd or non-zero.
func (s *ScannerFT) areaToAlpha(area int) uint32 {
	// The C Freetype implementation (version 2.3.12) does "alpha := area>>1"
	// without the +1. Round-to-nearest gives a more symmetric result than
	// round-down. The C implementation also returns 8-bit alpha, not 16-bit
	// alpha.
	a := (area + 1) >> 1
	if a < 0 {
		a = -a
	}
	alpha := uint32(a)
	if s.UseNonZeroWinding {
		if alpha > 0x0fff {
			alpha = 0x0fff
		}
	} else {
		// In even-odd mode the coverage is periodic with period 2*0x1000:
		// every second crossing flips the inside/outside state. Folding the
		// accumulated area modulo one period maps odd layers to filled and
		// even layers to empty, with the anti-aliasing ramp mirrored at each
		// boundary so coincident edges cancel exactly regardless of their
		// direction.
		alpha &= 0x1fff
		if alpha > 0x1000 {
			alpha = 0x2000 - alpha
		} else if alpha == 0x1000 {
			alpha = 0x0fff
		}
	}
	// alpha is now in the range [0x0000, 0x0fff]. Convert that 12-bit alpha to
	// 16-bit alpha.
	return alpha<<4 | alpha>>8
}

// paintSpan paints a single horizontal span onto the destination image using
// the Over Porter-Duff composition operator.
func (s *ScannerFT) paintSpan(sp ftSpan, b image.Rectangle) {
	if s.clip.Dx() != 0 && s.clip.Dy() != 0 {
		b = b.Intersect(s.clip)
	}
	if sp.Y < b.Min.Y {
		return
	}
	if sp.Y >= b.Max.Y {
		return
	}
	if sp.X0 < b.Min.X {
		sp.X0 = b.Min.X
	}
	if sp.X1 > b.Max.X {
		sp.X1 = b.Max.X
	}
	if sp.X0 >= sp.X1 {
		return
	}
	ma := sp.Alpha
	const m = 1<<16 - 1
	// Source samples are taken in source space, i.e. destination space minus
	// the Offset (ScannerGV always passes a zero Offset, but honor it).
	sx := sp.X0 - s.Offset.X
	sy := sp.Y - s.Offset.Y
	// Fast path for the common *image.RGBA destination.
	if dst, ok := s.Dest.(*image.RGBA); ok && s.Offset == (image.Point{}) {
		i0 := (sp.Y-dst.Rect.Min.Y)*dst.Stride + (sp.X0-dst.Rect.Min.X)*4
		i1 := i0 + (sp.X1-sp.X0)*4
		if _, isUniform := s.Source.(*image.Uniform); isUniform {
			sr, sg, sb, sa := s.Source.At(sx, sy).RGBA()
			for i := i0; i < i1; i += 4 {
				dr := uint32(dst.Pix[i+0])
				dg := uint32(dst.Pix[i+1])
				db := uint32(dst.Pix[i+2])
				da := uint32(dst.Pix[i+3])
				a := (m - (sa * ma / m)) * 0x101
				dst.Pix[i+0] = uint8((dr*a + sr*ma) / m >> 8)
				dst.Pix[i+1] = uint8((dg*a + sg*ma) / m >> 8)
				dst.Pix[i+2] = uint8((db*a + sb*ma) / m >> 8)
				dst.Pix[i+3] = uint8((da*a + sa*ma) / m >> 8)
			}
			return
		}
		cx := sx
		for i := i0; i < i1; i += 4 {
			sr, sg, sb, sa := s.Source.At(cx, sy).RGBA()
			cx++
			dr := uint32(dst.Pix[i+0])
			dg := uint32(dst.Pix[i+1])
			db := uint32(dst.Pix[i+2])
			da := uint32(dst.Pix[i+3])
			a := (m - (sa * ma / m)) * 0x101
			dst.Pix[i+0] = uint8((dr*a + sr*ma) / m >> 8)
			dst.Pix[i+1] = uint8((dg*a + sg*ma) / m >> 8)
			dst.Pix[i+2] = uint8((db*a + sb*ma) / m >> 8)
			dst.Pix[i+3] = uint8((da*a + sa*ma) / m >> 8)
		}
		return
	}
	// General path for any draw.Image. draw.Over matches the composition
	// used by ScannerGV's vector.Rasterizer.Draw.
	for x := sp.X0; x < sp.X1; x++ {
		dr, dg, db, da := s.Dest.At(x, sp.Y).RGBA()
		sr, sg, sb, sa := s.Source.At(x-s.Offset.X, sy).RGBA()
		a := (m - (sa * ma / m)) * 0x101
		r := uint8((dr*a + sr*ma) / m >> 8)
		g := uint8((dg*a + sg*ma) / m >> 8)
		bl := uint8((db*a + sb*ma) / m >> 8)
		al := uint8((da*a + sa*ma) / m >> 8)
		s.Dest.Set(x, sp.Y, color.RGBA{R: r, G: g, B: bl, A: al})
	}
}

// flush paints the accumulated span batch. The clip rectangle and the
// destination bounds are enforced here, the spans themselves being in
// destination (offset) coordinates.
func (s *ScannerFT) flush(spans []ftSpan) {
	for i := range spans {
		s.paintSpan(spans[i], s.Dest.Bounds())
	}
}

// Draw converts the accumulated curves into spans painted on the destination
// image. The spans are non-overlapping and sorted by Y and then X.
func (s *ScannerFT) Draw() {
	s.saveCell()
	t := 0
	for yi := 0; yi < len(s.cellIndex); yi++ {
		xi, cover := 0, 0
		for c := s.cellIndex[yi]; c != -1; c = s.cell[c].next {
			if cover != 0 && s.cell[c].xi > xi {
				alpha := s.areaToAlpha(cover * 64 * 2)
				if alpha != 0 {
					xi0, xi1 := xi, s.cell[c].xi
					if xi0 < 0 {
						xi0 = 0
					}
					if xi1 >= s.width {
						xi1 = s.width
					}
					if xi0 < xi1 {
						s.spanBuf[t] = ftSpan{yi + s.Offset.Y, xi0 + s.Offset.X, xi1 + s.Offset.X, alpha}
						t++
					}
				}
			}
			cover += s.cell[c].cover
			alpha := s.areaToAlpha(cover*64*2 - s.cell[c].area)
			xi = s.cell[c].xi + 1
			if alpha != 0 {
				xi0, xi1 := s.cell[c].xi, xi
				if xi0 < 0 {
					xi0 = 0
				}
				if xi1 >= s.width {
					xi1 = s.width
				}
				if xi0 < xi1 {
					s.spanBuf[t] = ftSpan{yi + s.Offset.Y, xi0 + s.Offset.X, xi1 + s.Offset.X, alpha}
					t++
				}
			}
			if t > len(s.spanBuf)-2 {
				s.flush(s.spanBuf[:t])
				t = 0
			}
		}
	}
	s.flush(s.spanBuf[:t])
}

// Clear cancels any previous accumulated scans
func (s *ScannerFT) Clear() {
	s.a = fixed.Point26_6{}
	s.xi = 0
	s.yi = 0
	s.area = 0
	s.cover = 0
	s.cell = s.cell[:0]
	for i := 0; i < len(s.cellIndex); i++ {
		s.cellIndex[i] = -1
	}
	const mxfi = fixed.Int26_6(math.MaxInt32)
	s.minX, s.minY, s.maxX, s.maxY = mxfi, mxfi, -mxfi, -mxfi
}

// SetBounds sets the maximum width and height of the rasterized image and
// calls Clear. The width and height are in pixels, not fixed.Int26_6 units.
func (s *ScannerFT) SetBounds(width, height int) {
	if width < 0 {
		width = 0
	}
	if height < 0 {
		height = 0
	}
	s.width = width
	s.cell = s.cellBuf[:0]
	if height > len(s.cellIndexBuf) {
		s.cellIndex = make([]int, height)
	} else {
		s.cellIndex = s.cellIndexBuf[:height]
	}
	s.Clear()
}

// NewScannerFT creates a new ScannerFT with the given bounds. It defaults to
// the non-zero winding rule, matching ScannerGV; call SetWinding(false) to
// fill with the even-odd rule.
func NewScannerFT(width, height int, dest draw.Image, targ image.Rectangle) *ScannerFT {
	s := new(ScannerFT)
	s.Dest = dest
	s.Targ = targ
	s.clipImage = &ClipImage{}
	s.clipImage.ColorFuncImage.Uniform.C = &color.RGBA{255, 0, 0, 255}
	s.Source = &s.clipImage.ColorFuncImage.Uniform
	s.Offset = image.Point{0, 0}
	s.SetBounds(width, height)
	s.UseNonZeroWinding = true
	return s
}
