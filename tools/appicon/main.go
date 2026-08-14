// Command appicon draws the application icon.
//
// Kept as code rather than a checked-in binary blob nobody can edit: the icon
// is a few shapes and a colour that has to stay in step with the accent blue
// used in the window.
//
//	go run ./tools/appicon > gui/build/appicon.png
//
// Wails turns that single PNG into the Windows .ico and macOS .icns at build
// time, so this is the only image the project keeps.
package main

import (
	"image"
	"image/color"
	"image/png"
	"log"
	"math"
	"os"
)

const (
	size   = 1024 // final icon size
	sample = 4    // supersampling factor, for antialiased corners
	canvas = size * sample
)

// The icon is a rack face: a grid of positions, one of them just captured.
// It has to survive being shrunk to 16px in a taskbar, so it is three shapes
// and two colours rather than anything literal.
var (
	blue  = color.NRGBA{0x00, 0x67, 0xc0, 0xff} // matches the Start button
	white = color.NRGBA{0xff, 0xff, 0xff, 0xff}
	green = color.NRGBA{0x3f, 0xb9, 0x50, 0xff} // the position just recorded
)

func main() {
	big := image.NewNRGBA(image.Rect(0, 0, canvas, canvas))

	// Background: a rounded square, the platform convention for app icons.
	fillRoundRect(big, 0, 0, canvas, canvas, 0.18*canvas, blue)

	// A 3x3 grid of positions inset from the edge.
	const cells = 3
	margin := 0.17 * canvas
	gap := 0.055 * canvas
	cell := (canvas - 2*margin - (cells-1)*gap) / cells
	radius := 0.22 * cell

	for row := 0; row < cells; row++ {
		for col := 0; col < cells; col++ {
			x := margin + float64(col)*(cell+gap)
			y := margin + float64(row)*(cell+gap)
			c := white
			// One cell reads as the position just captured, which is the whole
			// idea of the tool in a single shape.
			if row == 1 && col == 1 {
				c = green
			}
			fillRoundRect(big, x, y, cell, cell, radius, c)
		}
	}

	out := downsample(big, sample)
	if err := png.Encode(os.Stdout, out); err != nil {
		log.Fatal(err)
	}
}

// fillRoundRect paints a rounded rectangle. Corners are drawn hard and cleaned
// up by the downsample, which is why everything is rendered oversized.
func fillRoundRect(img *image.NRGBA, x, y, w, h, r float64, c color.NRGBA) {
	for py := int(y); py < int(y+h); py++ {
		for px := int(x); px < int(x+w); px++ {
			if insideRoundRect(float64(px)+0.5, float64(py)+0.5, x, y, w, h, r) {
				img.SetNRGBA(px, py, c)
			}
		}
	}
}

func insideRoundRect(px, py, x, y, w, h, r float64) bool {
	// Distance to the nearest corner centre, clamped into the inner rectangle.
	cx := math.Min(math.Max(px, x+r), x+w-r)
	cy := math.Min(math.Max(py, y+r), y+h-r)
	dx, dy := px-cx, py-cy
	return dx*dx+dy*dy <= r*r
}

// downsample box-filters the oversized canvas down to the final size, which is
// what turns the hard-edged shapes above into smooth ones.
func downsample(src *image.NRGBA, factor int) *image.NRGBA {
	w := src.Bounds().Dx() / factor
	h := src.Bounds().Dy() / factor
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	n := float64(factor * factor)

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var r, g, b, a float64
			for sy := 0; sy < factor; sy++ {
				for sx := 0; sx < factor; sx++ {
					p := src.NRGBAAt(x*factor+sx, y*factor+sy)
					// Weight colour by alpha so transparent pixels do not drag
					// the edges toward black.
					af := float64(p.A) / 255
					r += float64(p.R) * af
					g += float64(p.G) * af
					b += float64(p.B) * af
					a += float64(p.A)
				}
			}
			alpha := a / n
			out := color.NRGBA{A: uint8(math.Round(alpha))}
			if alpha > 0 {
				scale := 255 / alpha
				out.R = clamp(r / n * scale)
				out.G = clamp(g / n * scale)
				out.B = clamp(b / n * scale)
			}
			dst.SetNRGBA(x, y, out)
		}
	}
	return dst
}

func clamp(v float64) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(math.Round(v))
}
