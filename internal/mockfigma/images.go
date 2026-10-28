package mockfigma

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
)

var imageFormats = map[string]bool{"png": true, "jpg": true, "svg": true, "pdf": true}

func (s *Server) getImages(c *call) {
	f, ok := s.file(c, true)
	if !ok {
		return
	}
	v, ok := fileVersion(c, f)
	if !ok {
		return
	}
	q := c.r.URL.Query()
	ids := splitList(q.Get("ids"))
	if len(ids) == 0 {
		c.fail(http.StatusBadRequest, "Missing parameter: ids")
		return
	}
	format := q.Get("format")
	if format == "" {
		format = "png"
	}
	if !imageFormats[format] {
		c.fail(http.StatusBadRequest, "Invalid parameter: format must be one of jpg, png, svg, pdf")
		return
	}
	scale := 1.0
	if raw := q.Get("scale"); raw != "" {
		parsed, err := strconv.ParseFloat(raw, 64)
		if err != nil || math.IsNaN(parsed) || parsed < 0.01 || parsed > 4 {
			c.fail(http.StatusBadRequest, "Invalid parameter: scale must be between 0.01 and 4")
			return
		}
		scale = parsed
	}
	if s.renderFault(c, len(ids)) {
		return
	}
	nulled := s.nullRenders(c, ids)
	images := props{}
	for _, id := range ids {
		n := s.tree.byID[id]
		if n == nil || n.typ == "DOCUMENT" || n.hiddenInTree() || nulled[id] {
			images[id] = nil
			continue
		}
		query := url.Values{"scale": {strconv.FormatFloat(scale, 'f', -1, 64)}, "version": {v.id}}
		images[id] = c.base + "/_cdn/render/" + escapeNodeID(id) + "." + format + "?" + query.Encode()
	}
	c.ok(props{"err": nil, "images": images})
}

func (s *Server) renderFault(c *call, ids int) bool {
	f := s.faults.take(c.rules, func(f *fault) bool {
		return f.kind == faultRender500 || ids >= f.minIDs
	}, faultRenderTimeout, faultRender500)
	if f == nil {
		return false
	}
	if f.kind == faultRender500 {
		c.fail(http.StatusInternalServerError, "Unexpected rendering error, render could not be completed")
		return true
	}
	c.fail(http.StatusBadRequest, "Render timeout, try requesting fewer or smaller images")
	return true
}

func (s *Server) nullRenders(c *call, ids []string) map[string]bool {
	requested := map[string]bool{}
	for _, id := range ids {
		requested[id] = true
	}
	nulled := map[string]bool{}
	for _, f := range c.rules {
		if f.kind == faultNullRender && requested[f.target] && s.faults.fire(f) {
			nulled[f.target] = true
		}
	}
	return nulled
}

// escapeNodeID makes a node id safe as one path segment of a CDN URL.
func escapeNodeID(id string) string {
	return strings.NewReplacer(":", "%3A", ";", "%3B").Replace(id)
}

func (s *Server) getImageFills(c *call) {
	if _, ok := s.file(c, true); !ok {
		return
	}
	images := props{}
	for _, ref := range []string{ImageRefHero, ImageRefCover} {
		images[ref] = c.base + "/_cdn/fill/" + ref + ".png"
	}
	c.ok(props{"error": false, "status": 200, "meta": props{"images": images}})
}

// handleCDN serves every URL the mock hands out (renders, fills, thumbnails,
// avatars) as a tiny valid file of the extension's type. Like Figma's CDN it
// needs no token; cdn-expired makes it answer as an expired signed URL does.
func (s *Server) handleCDN(w http.ResponseWriter, r *http.Request) {
	rules, err := s.faults.forRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	expired := s.faults.take(rules, func(f *fault) bool {
		return strings.HasPrefix(r.URL.Path, f.target)
	}, faultCDNExpired)
	if expired != nil {
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><Error><Code>AccessDenied</Code><Message>Request has expired</Message></Error>`))
		return
	}
	body, contentType, ok := cdnPayload(strings.TrimPrefix(path.Ext(r.URL.Path), "."))
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	_, _ = w.Write(body)
}

var (
	pngBytes  = encodePNG()
	jpegBytes = encodeJPEG()
	svgBytes  = []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="1" height="1" viewBox="0 0 1 1"><rect width="1" height="1" fill="#3B5BF5"/></svg>` + "\n")
	pdfBytes  = minimalPDF()
)

func cdnPayload(ext string) ([]byte, string, bool) {
	switch ext {
	case "png":
		return pngBytes, "image/png", true
	case "jpg", "jpeg":
		return jpegBytes, "image/jpeg", true
	case "svg":
		return svgBytes, "image/svg+xml", true
	case "pdf":
		return pdfBytes, "application/pdf", true
	}
	return nil, "", false
}

func onePixel() image.Image {
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{R: 0x3B, G: 0x5B, B: 0xF5, A: 0xFF})
	return img
}

func encodePNG() []byte {
	var buf bytes.Buffer
	if err := png.Encode(&buf, onePixel()); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func encodeJPEG() []byte {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, onePixel(), nil); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

// minimalPDF builds a one-page PDF with a correct cross-reference table.
func minimalPDF() []byte {
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 1 1] >>",
	}
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects))
	for i, obj := range objects {
		offsets[i] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, obj)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, off := range offsets {
		fmt.Fprintf(&buf, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return buf.Bytes()
}
