package mockfigma

import (
	"bytes"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

const imagesPath = "/v1/images/" + FileKey

func TestImagesReturnURLsAndNulls(t *testing.T) {
	_, ts := newTestServer(t)
	ids := []string{NodeCheckout, NodeHiddenNote, "99:99", NodeDocument, NodeInstanceLabel}
	body := getOK(t, ts, imagesPath+"?ids="+url.QueryEscape(strings.Join(ids, ","))+"&format=svg&scale=2")

	if v, ok := body["err"]; !ok || v != nil {
		t.Fatalf("err = %v (present %v)", v, ok)
	}
	images := body["images"].(map[string]any)
	if len(images) != len(ids) {
		t.Fatalf("every requested id should be in the map, got %v", images)
	}
	for _, id := range []string{NodeHiddenNote, "99:99", NodeDocument} {
		if images[id] != nil {
			t.Errorf("%s should be null, got %v", id, images[id])
		}
	}
	renderURL, _ := images[NodeCheckout].(string)
	if !strings.HasPrefix(renderURL, ts.URL+"/_cdn/render/2%3A10.svg?") || !strings.Contains(renderURL, "scale=2") {
		t.Fatalf("render url = %q", renderURL)
	}
	if images[NodeInstanceLabel] == nil {
		t.Fatal("instance sublayer should render")
	}
	resp := do(t, http.MethodGet, renderURL, map[string]string{"X-Figma-Token": ""})
	if resp.status != http.StatusOK || resp.header.Get("Content-Type") != "image/svg+xml" || !bytes.HasPrefix(resp.body, []byte("<svg")) {
		t.Fatalf("CDN: %d %s %q", resp.status, resp.header.Get("Content-Type"), resp.body)
	}
}

func TestImagesValidateParams(t *testing.T) {
	_, ts := newTestServer(t)
	cases := map[string]int{
		"&scale=0":     http.StatusBadRequest,
		"&scale=0.001": http.StatusBadRequest,
		"&scale=4.5":   http.StatusBadRequest,
		"&scale=abc":   http.StatusBadRequest,
		"&scale=0.01":  http.StatusOK,
		"&scale=4":     http.StatusOK,
		"&format=gif":  http.StatusBadRequest,
		"&format=pdf":  http.StatusOK,
		"&format=jpg":  http.StatusOK,
		"&svg_outline_text=false&svg_include_id=true&svg_include_node_id=true&svg_simplify_stroke=false&contents_only=false&use_absolute_bounds=true": http.StatusOK,
	}
	for query, want := range cases {
		if resp := get(t, ts, imagesPath+"?ids="+NodeCheckout+query); resp.status != want {
			t.Errorf("%s: status %d, want %d (%s)", query, resp.status, want, resp.body)
		}
	}
	if resp := get(t, ts, imagesPath); resp.status != http.StatusBadRequest {
		t.Errorf("missing ids: status %d", resp.status)
	}
}

func TestCDNServesValidBytes(t *testing.T) {
	_, ts := newTestServer(t)
	cases := []struct {
		path, contentType string
		check             func([]byte) bool
	}{
		{"/_cdn/render/2%3A10.png", "image/png", func(b []byte) bool { _, err := png.Decode(bytes.NewReader(b)); return err == nil }},
		{"/_cdn/render/2%3A10.jpg", "image/jpeg", func(b []byte) bool { _, err := jpeg.Decode(bytes.NewReader(b)); return err == nil }},
		{"/_cdn/render/2%3A10.svg", "image/svg+xml", func(b []byte) bool { return bytes.HasPrefix(b, []byte("<svg")) }},
		{"/_cdn/render/2%3A10.pdf", "application/pdf", func(b []byte) bool {
			return bytes.HasPrefix(b, []byte("%PDF-")) && bytes.HasSuffix(b, []byte("%%EOF\n"))
		}},
		{"/_cdn/fill/" + ImageRefHero + ".png", "image/png", func(b []byte) bool { return bytes.HasPrefix(b, []byte("\x89PNG")) }},
	}
	for _, tc := range cases {
		resp := do(t, http.MethodGet, ts.URL+tc.path, map[string]string{"X-Figma-Token": ""})
		if resp.status != http.StatusOK || resp.header.Get("Content-Type") != tc.contentType || !tc.check(resp.body) {
			t.Errorf("%s: %d %s", tc.path, resp.status, resp.header.Get("Content-Type"))
		}
	}
	if resp := get(t, ts, "/_cdn/render/2%3A10.gif"); resp.status != http.StatusNotFound {
		t.Errorf("unknown extension: status %d", resp.status)
	}
}

func TestImageFills(t *testing.T) {
	_, ts := newTestServer(t)
	for _, key := range []string{FileKey, BranchKey} {
		body := getOK(t, ts, "/v1/files/"+key+"/images")
		if body["error"] != false || body["status"] != float64(200) {
			t.Fatalf("envelope = %v", body)
		}
		fillURL, _ := dig(t, body, "meta", "images", ImageRefHero).(string)
		if fillURL != ts.URL+"/_cdn/fill/"+ImageRefHero+".png" {
			t.Fatalf("fill url = %q", fillURL)
		}
		if dig(t, body, "meta", "images", ImageRefCover) == nil {
			t.Fatal("cover fill missing")
		}
	}
}
