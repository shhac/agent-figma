package mockfigma

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"math"
	"net/http"
	"path"
	"strconv"
	"strings"
)

// MaxRenderSide caps each side of a PNG or JPEG render, in pixels.
const MaxRenderSide = 4096

var rasterFormats = map[string]bool{"png": true, "jpg": true, "jpeg": true}

// renderRaster draws the node a /_cdn/render/<id>.<png|jpg> URL names. The
// query carries key (default the main file), version (default the newest)
// and scale (default 1); an unknown key, version or node is a 404.
func (s *Server) renderRaster(r *http.Request, ext string) ([]byte, int) {
	q := r.URL.Query()
	key := q.Get("key")
	if key == "" {
		key = FileKey
	}
	f, ok := s.snapshot(key)
	if !ok {
		return nil, http.StatusNotFound
	}
	v := f.current()
	if want := q.Get("version"); want != "" {
		if v, ok = f.version(want); !ok {
			return nil, http.StatusNotFound
		}
	}
	scale := 1.0
	if raw := q.Get("scale"); raw != "" {
		parsed, err := strconv.ParseFloat(raw, 64)
		if err != nil || math.IsNaN(parsed) || parsed <= 0 {
			return nil, http.StatusBadRequest
		}
		scale = parsed
	}
	id := strings.TrimSuffix(path.Base(r.URL.Path), path.Ext(r.URL.Path))
	n := v.tree.byID[id]
	if n == nil || n.typ == "DOCUMENT" {
		return nil, http.StatusNotFound
	}
	img := rasterize(n, scale)
	var buf bytes.Buffer
	var err error
	if ext == "png" {
		err = png.Encode(&buf, img)
	} else {
		err = jpeg.Encode(&buf, img, &jpeg.Options{Quality: 95})
	}
	if err != nil {
		return nil, http.StatusInternalServerError
	}
	return buf.Bytes(), http.StatusOK
}

// rasterize is the mock's deterministic stand-in for Figma's renderer: an
// image of round(width*scale) x round(height*scale) from the node's
// absoluteBoundingBox (each side clamped to 1..MaxRenderSide), filled with
// the node's first visible SOLID fill (white without one), then every visible
// descendant with a SOLID fill drawn as an opaque rectangle at its scaled
// position relative to the node, in tree order. Alpha, opacity, gradients,
// images, strokes, effects, corner radii and clipping are ignored, so a test
// can predict every pixel from the tree alone.
func rasterize(n *node, scale float64) *image.RGBA {
	origin, _ := boundingBox(n)
	img := image.NewRGBA(image.Rect(0, 0, renderSide(origin.Dx()*scale), renderSide(origin.Dy()*scale)))
	background, ok := firstSolidFill(n)
	if !ok {
		background = color.RGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}
	}
	draw.Draw(img, img.Bounds(), image.NewUniform(background), image.Point{}, draw.Src)
	for _, k := range n.children {
		paintSubtree(img, k, origin, scale)
	}
	return img
}

func paintSubtree(img *image.RGBA, n *node, origin floatRect, scale float64) {
	if n.hidden() {
		return
	}
	box, hasBox := boundingBox(n)
	if fill, ok := firstSolidFill(n); ok && hasBox {
		area := image.Rect(
			roundInt((box.x0-origin.x0)*scale), roundInt((box.y0-origin.y0)*scale),
			roundInt((box.x1-origin.x0)*scale), roundInt((box.y1-origin.y0)*scale),
		).Intersect(img.Bounds())
		draw.Draw(img, area, image.NewUniform(fill), image.Point{}, draw.Src)
	}
	for _, k := range n.children {
		paintSubtree(img, k, origin, scale)
	}
}

type floatRect struct{ x0, y0, x1, y1 float64 }

func (r floatRect) Dx() float64 { return r.x1 - r.x0 }
func (r floatRect) Dy() float64 { return r.y1 - r.y0 }

// boundingBox reads absoluteBoundingBox; a node without one (a page) renders
// as a single pixel.
func boundingBox(n *node) (floatRect, bool) {
	box, ok := n.props["absoluteBoundingBox"].(props)
	if !ok {
		return floatRect{x1: 1, y1: 1}, false
	}
	x, y := number(box["x"]), number(box["y"])
	return floatRect{x0: x, y0: y, x1: x + number(box["width"]), y1: y + number(box["height"])}, true
}

func firstSolidFill(n *node) (color.RGBA, bool) {
	fills, _ := n.props["fills"].([]any)
	for _, raw := range fills {
		paint, _ := raw.(props)
		if visible, ok := paint["visible"].(bool); ok && !visible {
			continue
		}
		if paint["type"] != "SOLID" {
			continue
		}
		c, _ := paint["color"].(props)
		return color.RGBA{R: channel(c["r"]), G: channel(c["g"]), B: channel(c["b"]), A: 0xFF}, true
	}
	return color.RGBA{}, false
}

func channel(v any) uint8 {
	return uint8(math.Round(math.Max(0, math.Min(1, number(v))) * 255))
}

// number accepts the numeric shapes a node property can hold: fixture
// literals and values decoded from JSON edits.
func number(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	}
	return 0
}

func renderSide(v float64) int {
	return min(max(roundInt(v), 1), MaxRenderSide)
}

func roundInt(v float64) int { return int(math.Round(v)) }
