package imagediff

import (
	"image"
	"image/color"
	"image/draw"
	"testing"
)

func solid(w, h int, c color.Color) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), image.NewUniform(c), image.Point{}, draw.Src)
	return img
}

func TestIdenticalImagesMatch(t *testing.T) {
	a := solid(100, 50, color.RGBA{10, 20, 30, 255})
	res := Compare(a, a, 24)
	if res.MatchPercent != 100 || len(res.Regions) != 0 {
		t.Fatalf("identical: %+v", res)
	}
}

func TestDifferenceIsLocalised(t *testing.T) {
	design := solid(200, 100, color.White)
	actual := solid(200, 100, color.White)
	draw.Draw(actual, image.Rect(120, 40, 160, 60), image.NewUniform(color.Black), image.Point{}, draw.Src)
	res := Compare(design, actual, 24)
	if len(res.Regions) != 1 || res.Mismatched != 40*20 {
		t.Fatalf("one region expected: %+v", res.Regions)
	}
	b := res.Regions[0].Box
	if cx, cy := b.Center(); cx < 120 || cx > 160 || cy < 40 || cy > 60 {
		t.Fatalf("region misplaced: %+v", b)
	}
}

func TestSmallNoiseUnderThresholdIgnored(t *testing.T) {
	design := solid(50, 50, color.RGBA{100, 100, 100, 255})
	actual := solid(50, 50, color.RGBA{110, 95, 100, 255})
	if res := Compare(design, actual, 24); res.Mismatched != 0 {
		t.Fatalf("noise counted: %d", res.Mismatched)
	}
}

func TestTransparencyFlattensOntoWhite(t *testing.T) {
	transparent := image.NewRGBA(image.Rect(0, 0, 10, 10))
	if res := Compare(transparent, solid(10, 10, color.White), 24); res.Mismatched != 0 {
		t.Fatal("transparent should compare as white")
	}
}
