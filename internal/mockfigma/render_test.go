package mockfigma

import (
	"bytes"
	"image"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func renderURLFor(t *testing.T, ts *httptest.Server, id, query string) string {
	t.Helper()
	body := getOK(t, ts, imagesPath+"?ids="+url.QueryEscape(id)+query)
	u, _ := dig(t, body, "images", id).(string)
	if u == "" {
		t.Fatalf("no render url for %s: %v", id, body)
	}
	return u
}

// renderPixels asks /v1/images for a PNG of id and decodes what the CDN
// serves for the URL it hands back.
func renderPixels(t *testing.T, ts *httptest.Server, id, query string) image.Image {
	t.Helper()
	resp := do(t, http.MethodGet, renderURLFor(t, ts, id, "&format=png"+query), map[string]string{"X-Figma-Token": ""})
	if resp.status != http.StatusOK || resp.header.Get("Content-Type") != "image/png" {
		t.Fatalf("CDN: %d %s %s", resp.status, resp.header.Get("Content-Type"), resp.body)
	}
	img, err := png.Decode(bytes.NewReader(resp.body))
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func rgbAt(img image.Image, x, y int) [3]uint8 {
	r, g, b, _ := img.At(x, y).RGBA()
	return [3]uint8{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8)}
}

func TestRenderSizeFollowsBoundsAndScale(t *testing.T) {
	_, ts := newTestServer(t)
	cases := []struct {
		id, scale string
		w, h      int
	}{
		{NodeCheckout, "2", 750, 1880},
		{NodeCheckout, "0.5", 188, 470}, // 187.5 rounds half away from zero
		{NodeCard, "1.5", 491, 180},     // 490.5
		{NodeButtonDefault, "1", 120, 40},
		{NodeStatusDot, "0.01", 1, 1}, // 0.08 x 0.08, never below one pixel
	}
	for _, tc := range cases {
		b := renderPixels(t, ts, tc.id, "&scale="+tc.scale).Bounds()
		if b.Dx() != tc.w || b.Dy() != tc.h {
			t.Errorf("%s @%s = %dx%d, want %dx%d", tc.id, tc.scale, b.Dx(), b.Dy(), tc.w, tc.h)
		}
	}
}

func TestRenderCapsEachSide(t *testing.T) {
	s, ts := newTestServer(t)
	if _, err := s.Edit(FileKey, Edit{Op: EditSet, Node: NodeBadge, Field: "absoluteBoundingBox",
		Value: rect(0, 0, 3000, 10)}); err != nil {
		t.Fatal(err)
	}
	if b := renderPixels(t, ts, NodeBadge, "&scale=4").Bounds(); b.Dx() != MaxRenderSide || b.Dy() != 40 {
		t.Fatalf("capped render = %dx%d", b.Dx(), b.Dy())
	}
}

func TestRenderDrawsSolidFills(t *testing.T) {
	_, ts := newTestServer(t)
	img := renderPixels(t, ts, NodeCheckout, "&scale=1")
	white, ink := [3]uint8{255, 255, 255}, [3]uint8{17, 19, 24}
	checks := []struct {
		x, y int
		want [3]uint8
		why  string
	}{
		{2, 2, white, "Checkout's own fill"},
		{30, 85, ink, "Total text (24,80 327x24) in its text colour"},
		{200, 530, white, "Hidden note (24,516 327x40) is invisible"},
		{60, 550, white, "Swatch inside Export me, whose gradient is not drawn"},
		{30, 520, white, "Export me's gradient fill is skipped"},
		{320, 330, [3]uint8{230, 57, 70}, "Badge, nested inside Frame 12"},
	}
	for _, c := range checks {
		if got := rgbAt(img, c.x, c.y); got != c.want {
			t.Errorf("(%d,%d) %s: got %v, want %v", c.x, c.y, c.why, got, c.want)
		}
	}
	jpg := do(t, http.MethodGet, renderURLFor(t, ts, NodeCard, "&format=jpg"), nil)
	decoded, err := jpeg.Decode(bytes.NewReader(jpg.body))
	if err != nil || decoded.Bounds().Dx() != 327 || decoded.Bounds().Dy() != 120 {
		t.Fatalf("jpg render: %v %v", err, decoded)
	}
}

// An edit to one child's paint must change pixels only inside that child's
// rectangle, so a compare can localise a difference.
func TestRenderEditChangesOnlyTheChildsPixels(t *testing.T) {
	cases := []struct {
		node string
		area image.Rectangle // relative to Card (24,708)
	}{
		{NodeStatusDot, image.Rect(327-24, 728-708, 335-24, 736-708)},
		{NodePriceText, image.Rect(40-24, 724-708, 104-24, 748-708)},
	}
	for _, tc := range cases {
		s, ts := newTestServer(t)
		before := renderPixels(t, ts, NodeCard, "&scale=1")
		if _, err := s.Edit(FileKey, Edit{Op: EditSet, Node: tc.node, Field: "fills", Value: paints(solid(colorAlert))}); err != nil {
			t.Fatal(err)
		}
		after := renderPixels(t, ts, NodeCard, "&scale=1")
		if after.Bounds() != before.Bounds() {
			t.Fatalf("%s: size changed %v -> %v", tc.node, before.Bounds(), after.Bounds())
		}
		changed := image.Rectangle{}
		b := before.Bounds()
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				if rgbAt(before, x, y) != rgbAt(after, x, y) {
					changed = changed.Union(image.Rect(x, y, x+1, y+1))
				}
			}
		}
		if changed != tc.area {
			t.Errorf("%s: changed pixels span %v, want exactly %v", tc.node, changed, tc.area)
		}
		old := renderPixels(t, ts, NodeCard, "&scale=1&version="+InitialVersion)
		if rgbAt(old, tc.area.Min.X, tc.area.Min.Y) != rgbAt(before, tc.area.Min.X, tc.area.Min.Y) {
			t.Errorf("%s: the old version's render should not change", tc.node)
		}
	}
}

func TestRenderURLCarriesKeyVersionAndScale(t *testing.T) {
	_, ts := newTestServer(t)
	u := renderURLFor(t, ts, NodeInstanceLabel, "&scale=3&version="+InitialVersion)
	want := ts.URL + "/_cdn/render/I2%3A11%3B1%3A14.png?key=" + FileKey + "&scale=3&version=" + InitialVersion
	if u != want {
		t.Fatalf("render url = %q, want %q", u, want)
	}
	if b := renderPixels(t, ts, NodeInstanceLabel, "&scale=3").Bounds(); b.Dx() != 264 || b.Dy() != 60 {
		t.Fatalf("instance sublayer render = %v", b)
	}
	for _, bad := range []string{
		"/_cdn/render/99%3A99.png",
		"/_cdn/render/2%3A10.png?key=NOPE",
		"/_cdn/render/2%3A10.png?version=123",
	} {
		if resp := do(t, http.MethodGet, ts.URL+bad, nil); resp.status != http.StatusNotFound {
			t.Errorf("%s: status %d", bad, resp.status)
		}
	}
}
