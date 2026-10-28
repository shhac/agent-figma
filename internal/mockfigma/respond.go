package mockfigma

import (
	"encoding/json"
	"net/http"
	"strings"
)

// errorStyle is which of Figma's two error bodies an endpoint uses, per the
// OpenAPI spec: {status, err} for most, {error: true, status, message} for
// variables and dev resources, and a mix for the library endpoints, whose 404
// alone uses the boolean form.
type errorStyle int

const (
	errMessage errorStyle = iota
	errBoolean
	errLibrary
)

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(payload)
}

func writeError(w http.ResponseWriter, style errorStyle, status int, msg string) {
	if style == errBoolean || (style == errLibrary && status == http.StatusNotFound) {
		writeJSON(w, status, props{"error": true, "status": status, "message": msg})
		return
	}
	writeJSON(w, status, props{"status": status, "err": msg})
}

// call is one authenticated API request on its way through a handler.
type call struct {
	w     http.ResponseWriter
	r     *http.Request
	style errorStyle
	base  string
	rules []*fault
}

func (c *call) ok(payload any) { writeJSON(c.w, http.StatusOK, payload) }

func (c *call) fail(status int, msg string) { writeError(c.w, c.style, status, msg) }

func (c *call) notFound() { c.fail(http.StatusNotFound, "Not found") }

func splitList(raw string) []string {
	out := []string{}
	for _, part := range strings.Split(raw, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}
