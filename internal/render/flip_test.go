package render

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"testing"
)

func TestFlipDownFrames(t *testing.T) {
	previous, next := image.NewRGBA(image.Rect(0, 0, 130, 38)), image.NewRGBA(image.Rect(0, 0, 130, 38))
	draw.Draw(previous, previous.Rect, image.NewUniform(color.RGBA{200, 0, 0, 255}), image.Point{}, draw.Src)
	draw.Draw(next, next.Rect, image.NewUniform(color.RGBA{0, 200, 0, 255}), image.Point{}, draw.Src)
	before := append([]byte(nil), previous.Pix...)
	after := append([]byte(nil), next.Pix...)
	if FlipDown(previous, next, 0) != previous || FlipDown(previous, next, 1) != next {
		t.Fatal("endpoints must match the original and final frames")
	}
	for _, progress := range []float64{.1, .25, .5, .75, .9} {
		frame := FlipDown(previous, next, progress)
		if frame.Rect != previous.Rect {
			t.Fatalf("frame changed footprint: %v", frame.Rect)
		}
		if progress >= .25 && frame.RGBAAt(1, 0).G != 200 {
			t.Fatal("the new top must be revealed first")
		}
		if progress <= .75 && frame.RGBAAt(1, 37).R != 200 {
			t.Fatal("the old bottom must remain until the new flap reaches it")
		}
	}
	if !bytes.Equal(previous.Pix, before) || !bytes.Equal(next.Pix, after) {
		t.Fatal("animation modified the cached inputs")
	}
}
