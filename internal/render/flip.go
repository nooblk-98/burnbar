package render

import (
	"image"
	"image/draw"
	"math"

	xdraw "golang.org/x/image/draw"
)

// FlipDown turns the upper flap down into the lower flap, like a split-flap clock.
// The caption and both quota bars change together. Inputs remain untouched.
func FlipDown(previous, next *image.RGBA, progress float64) *image.RGBA {
	if progress <= 0 || math.IsNaN(progress) {
		return previous
	}
	if progress >= 1 || previous.Rect != next.Rect {
		return next
	}
	img := image.NewRGBA(previous.Rect)
	w, h := img.Rect.Dx(), img.Rect.Dy()
	mid := h / 2
	top, bottom := image.Rect(0, 0, w, mid), image.Rect(0, mid, w, h)
	draw.Draw(img, top, next, image.Point{}, draw.Src)
	draw.Draw(img, bottom, previous, image.Pt(0, mid), draw.Src)
	// Smooth acceleration and deceleration, with perspective compression at the hinge.
	t := progress * progress * (3 - 2*progress)
	var src *image.RGBA
	var sr, dst image.Rectangle
	var shade float64
	if t < .5 {
		flapH := int(math.Round(float64(mid) * math.Cos(math.Pi*t)))
		src, sr, dst = previous, top, image.Rect(0, mid-flapH, w, mid)
		shade = 1 - .38*math.Sin(math.Pi*t)
	} else {
		flapH := int(math.Round(float64(h-mid) * -math.Cos(math.Pi*t)))
		src, sr, dst = next, bottom, image.Rect(0, mid, w, mid+flapH)
		shade = 1 - .38*math.Sin(math.Pi*t)
	}
	if !dst.Empty() {
		xdraw.CatmullRom.Scale(img, dst, src, sr, draw.Src, nil)
		for y := dst.Min.Y; y < dst.Max.Y; y++ {
			for x := dst.Min.X; x < dst.Max.X; x++ {
				i := img.PixOffset(x, y)
				for c := 0; c < 3; c++ {
					img.Pix[i+c] = uint8(float64(img.Pix[i+c]) * shade)
				}
			}
		}
	}
	return img
}
