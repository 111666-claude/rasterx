# rasterx

Rasterx is a golang rasterizer that implements path stroking functions capable of SVG 2.0 compliant 'arc' joins and explicit loop closing. 



* Paths can be explicity closed or left open, resulting in a line join or end caps. 
* Arc joins are supported, which causes the extending edge from a Bezier curve to follow the radius of curvature at the end point rather than a straight line miter, resulting in a more fluid looking join. 
* Not specified in the SVG2.0 spec., but supported in rasterx is the arc-clip join, which is the arc join analog of a miter-clip join, both of which end the miter at a specified distance, rather than all or nothing.
* Several cap and gap functions in addition to those specified by SVG2.0 are implemented, specifically quad and cubic caps and gaps.
* Line start and end capping functions can be different.
* Closed, simple contours can be stroked centered (the default), fully inside or fully outside the path via `Stroker.SetStrokeMode`, so an icon border can grow outward while its filled interior keeps its original size.

## Inside, centered and outside stroke placement

`SetStrokeMode(StrokeCenter | StrokeInside | StrokeOutside)` selects where the
stroke band of width `w` sits relative to a closed, non-self-intersecting
contour:

| Mode            | Port side offset | Starboard side offset | Result |
|-----------------|------------------|-----------------------|--------|
| `StrokeCenter`  | `w/2`            | `w/2`                 | Original SVG behavior; unchanged default. |
| `StrokeInside`  | `w` on the interior side, `0` on the other | | The exterior boundary keeps the contour's original size. |
| `StrokeOutside` | `w` on the exterior side, `0` on the other | | The interior boundary keeps the contour's original size, so the fill region is never overpainted. |

The interior side is determined per closed subpath from its signed area, so
clockwise and counter-clockwise traversals of the same contour produce
equivalent strokes; holes (oppositely oriented subpaths) are handled
individually. Lines and flattened Bezier segments share the same offsets, and
join mode, cap/gap functions and the miter limit (scaled with the active side
offset) behave as in centered stroking.

Placement rules:

* Inside/outside modes require **closed** subpaths; an open subpath panics with
  `ErrOpenPathStroke`. Open paths keep using centered stroking with end caps.
* A closed contour with (near) zero signed area panics with
  `ErrZeroAreaStroke`, since it has no determinable interior.
* A negative stroke width panics with `ErrNegativeWidth`; a zero width renders
  nothing.
* Inside/outside placement cannot be combined with dash patterns and panics
  with `ErrDashedStrokePlacement`; use `StrokeCenter` for dashed strokes.
* A stroke wider than the contour itself (for example a very narrow inside
  stroke) is still rendered; offset edges simply collapse onto the source
  contour where the geometry requires it.


![rasterx example](/doc/TestShapes4.svg.png?raw=true "Rasterx Example")

The above image shows the effect of using different join modes for a stroked curving path. The top stroked path uses miter (green) or arc (red, yellow, orange) join functions with high miter limit. The middle and lower path shows the effect of using the miter-clip and arc-clip joins, repectively, with different miter-limit values. The black chevrons at the top show different cap and gap functions.

## Scanner interface

Rasterx takes the path description of lines, bezier curves, and drawing parameters, and converts them into a set of straight line segments before rasterizing the lines to an image using some method of antialiasing. Rasterx abstracts this last step through the Scanner interface. There are two different structs that satisfy the Scanner interface; ScannerGV and [ScannerFT](https://github.com/srwiley/scanFT). ScannerGV wraps the rasterizer found in the golang.org/x/image/vector package. ScannerFT contains a modified version of the antialiaser found in the [golang freetype](https://github.com/golang/freetype) translation. These use different functions to connect an image to the antialiaser. ScannerFT uses a Painter to translate the raster onto the image, and ScannerGV uses the vector's Draw method with a source image and uses the path as an alpha mask. Please see the test files for examples. At this time, the ScannerFT is a bit faster as compared to ScannerGV for larger and less complicated images, while ScannerGV can be faster for smaller and more complex images. Also ScannerGV does not allow for using the even-odd winding rule, which is something the SVG specification uses. Since ScannerFT is subject to freetype style licensing rules, it lives [here](https://github.com/srwiley/scanFT) in a separate repository and must be imported into your project seperately. ScannerGV is included in the rasterx package, and has more go-friendly licensing. 

Below are the results of some benchmarks performed on a sample shape (the letter Q ). The first test is the time it takes to scan the image after all the curves have been flattened. The second test is the time it takes to flatten, and scan a simple filled image. The last test is the time it takes to flatten a stroked and dashed outline of the shape and scan it. Results for three different image sizes are shown.


```
128x128 Image
Test                        Rep       Time
BenchmarkScanGV-16          5000      287180 ns/op
BenchmarkFillGV-16          5000      339831 ns/op
BenchmarkDashGV-16          2000      968265 ns/op

BenchmarkScanFT-16    	   20000       88118 ns/op
BenchmarkFillFT-16    	    5000      214370 ns/op
BenchmarkDashFT-16    	    1000     2063797 ns/op

256x256 Image
Test                        Rep       Time
BenchmarkScanGV-16          2000     1188452 ns/op
BenchmarkFillGV-16          1000     1277268 ns/op
BenchmarkDashGV-16          500      2238169 ns/op

BenchmarkScanFT-16    	    5000      290685 ns/op
BenchmarkFillFT-16    	    3000      446329 ns/op
BenchmarkDashFT-16    	     500     2923512 ns/op

512x512 Image
Test                        Rep       Time
BenchmarkScanGV-16           500     3341038 ns/op
BenchmarkFillGV-16           500     4032213 ns/op
BenchmarkDashGV-16           200     6003355 ns/op

BenchmarkScanFT-16    	    5000      292884 ns/op
BenchmarkFillFT-16    	    3000      449582 ns/op
BenchmarkDashFT-16    	     500     2800493 ns/op
```

The package uses an interface called Rasterx, which is satisfied by three structs, Filler, Stroker and Dasher.  The Filler flattens Bezier curves into lines and uses an anonymously composed Scanner for the antialiasing step. The Stroker embeds a Filler and adds path stroking, and the Dasher embedds a Stroker and adds the ability to create dashed stroked curves.


![rasterx Scheme](/doc/schematic.png?raw=true "Rasterx Scheme")

Each of the Filler, Dasher, and Stroker can function on their own and each implement the Rasterx interface, so if you need just the curve filling but no stroking capability, you only need a Filler. On the other hand if you have created a Dasher and want to use it to Fill, you can just do this:

```golang
filler := &dasher.Filler
```
Now filler is a filling rasterizer. Please see rasterx_test.go for examples.


### Non-standard library dependencies
rasterx requires the following imports which are not included in the go standard library:

* golang.org/x/image/math/fixed
* golang.org/x/image/vector

These can be included in your gopath by the following 'get' commands:

* "go get golang.org/x/image/vector"
* "go get golang.org/x/image/math/fixed"

If you want to use the freetype style antialiaser, 'go get' or clone into your workspace the scanFT package:

* github.com/srwiley/scanFT 

