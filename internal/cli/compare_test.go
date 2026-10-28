package cli

import (
	"image"
	"testing"

	"github.com/shhac/agent-figma/internal/figma"
)

func TestInferScaleUsesRenderBoundsAndSnaps(t *testing.T) {
	n := figma.Node{
		AbsoluteBoundingBox:  &figma.Rect{X: 0, Y: 0, Width: 1144, Height: 1032},
		AbsoluteRenderBounds: &figma.Rect{X: -38, Y: -14, Width: 1220, Height: 1108},
	}
	if got := inferScale(2440, n); got != 2 {
		t.Fatalf("render-bounds width at 2x: got %v", got)
	}
	if got := inferScale(2288, n); got != 2 {
		t.Fatalf("box width at 2x: got %v", got)
	}
	if got := inferScale(1716, n); got != 1.5 {
		t.Fatalf("1.5x: got %v", got)
	}
	if got := inferScale(1300, n); got < 1.1 || got > 1.2 {
		t.Fatalf("an unusual scale is used as measured: got %v", got)
	}
}

func TestAlignDesignCropsShadowsAway(t *testing.T) {
	n := figma.Node{
		AbsoluteBoundingBox:  &figma.Rect{X: 100, Y: 100, Width: 50, Height: 20},
		AbsoluteRenderBounds: &figma.Rect{X: 90, Y: 95, Width: 70, Height: 35},
	}
	rendered := image.NewRGBA(image.Rect(0, 0, 140, 70))
	img, origin := alignDesign(rendered, image.Rect(0, 0, 100, 40), n, 2)
	if img.Bounds().Dx() != 100 || img.Bounds().Dy() != 40 || origin.X != 100 {
		t.Fatalf("cropped to the box: %v %v", img.Bounds(), origin)
	}
	img, origin = alignDesign(rendered, image.Rect(0, 0, 140, 70), n, 2)
	if img.Bounds().Dx() != 140 || origin.X != 90 {
		t.Fatalf("a screenshot with the shadow keeps the full render: %v %v", img.Bounds(), origin)
	}
}
