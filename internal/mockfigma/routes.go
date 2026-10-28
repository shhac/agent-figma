package mockfigma

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
)

type apiRoute struct {
	path  string
	style errorStyle
	note  string
	serve func(*Server, *call)
}

func apiRoutes() []apiRoute {
	return []apiRoute{
		{"/v1/me", errMessage, "", (*Server).getMe},
		{"/v1/files/{key}", errMessage, "ids, depth, version, geometry=paths, branch_data", (*Server).getFile},
		{"/v1/files/{key}/nodes", errMessage, "ids (required), depth relative to each node, version, geometry", (*Server).getNodes},
		{"/v1/images/{key}", errMessage, "ids, scale 0.01-4, format png|jpg|svg|pdf, svg_*, contents_only, use_absolute_bounds, version", (*Server).getImages},
		{"/v1/files/{key}/images", errMessage, "image fill URLs", (*Server).getImageFills},
		{"/v1/files/{key}/meta", errMessage, "", (*Server).getMeta},
		{"/v1/files/{key}/versions", errMessage, "page_size, before, after", (*Server).getVersions},
		{"/v1/files/{key}/comments", errMessage, "", (*Server).getComments},
		{"/v1/files/{key}/components", errLibrary, "main file key only", (*Server).getFileComponents},
		{"/v1/files/{key}/component_sets", errLibrary, "main file key only", (*Server).getFileComponentSets},
		{"/v1/files/{key}/styles", errLibrary, "main file key only", (*Server).getFileStyles},
		{"/v1/components/{key}", errLibrary, "", (*Server).getComponent},
		{"/v1/component_sets/{key}", errLibrary, "", (*Server).getComponentSet},
		{"/v1/styles/{key}", errLibrary, "", (*Server).getStyle},
		{"/v1/files/{key}/variables/local", errBoolean, "", (*Server).getLocalVariables},
		{"/v1/files/{key}/variables/published", errBoolean, "", (*Server).getPublishedVariables},
		{"/v1/files/{key}/dev_resources", errBoolean, "node_ids; main file key only", (*Server).getDevResources},
	}
}

func (s *Server) routes() {
	for _, rt := range apiRoutes() {
		serve := rt.serve
		s.mux.Handle("GET "+rt.path, s.api(rt.style, func(c *call) { serve(s, c) }))
	}
	s.mux.HandleFunc("GET /{$}", s.handleIndex)
	s.mux.HandleFunc("GET /_mock/stats", s.handleStats)
	s.mux.HandleFunc("POST /_mock/reset", s.handleReset)
	s.mux.HandleFunc("GET /_mock/faults", s.handleGetFaults)
	s.mux.HandleFunc("POST /_mock/faults", s.handleSetFaults)
	s.mux.HandleFunc("DELETE /_mock/faults", s.handleClearFaults)
	s.mux.HandleFunc("POST /_mock/version/bump", s.handleBump)
	s.mux.HandleFunc("POST /_mock/edit", s.handleEdit)
	s.mux.HandleFunc("GET /_cdn/", s.handleCDN)
	s.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, errMessage, http.StatusNotFound, "Not found")
	})
}

// Routes is the route map printed by `mockfigma --routes` and GET /.
func Routes() []string {
	out := []string{}
	for _, rt := range apiRoutes() {
		line := "GET    " + rt.path
		if rt.note != "" {
			line += "    (" + rt.note + ")"
		}
		out = append(out, line)
	}
	return append(out,
		"",
		"Auth: X-Figma-Token or Authorization: Bearer, any non-empty value; \""+ExpiredToken+"\" is rejected with 403.",
		"File keys: "+FileKey+" (main), "+BranchKey+" (branch).",
		"",
		"Mock controls (no auth):",
		"GET    /                       route map",
		"GET    /_mock/stats            {tier1, tier2, tier3, by_path, requests}",
		"POST   /_mock/reset            clear counters and faults, restore versions",
		"GET    /_mock/faults           standing rules and how often each fired",
		"POST   /_mock/faults?rules=    arm standing rules (comma-separated)",
		"DELETE /_mock/faults           clear standing rules",
		"POST   /_mock/version/bump     new version, same content (?key=, default the main file)",
		"POST   /_mock/edit             new version with edits applied (?key=; body: JSON list of {op: set|remove|add|move, node, field, value, parent, index, node_json})",
		"GET    /_cdn/<kind>/<name>.<png|jpg|svg|pdf>   render, fill, and thumbnail bytes",
		"GET    /_cdn/render/<id>.<png|jpg>?key=&version=&scale=   render drawn at round(w*scale) x round(h*scale)",
		"",
		"Fault rules (POST /_mock/faults?rules= or X-Mock-Fault header):",
		"  429:<tier1|tier2|tier3|/path-prefix>[:retry=<s>][:type=low|high][:plan=<tier>][:count=<n>]",
		"  too-large:<path-prefix>[:min_nodes=<n>][:count=<n>]",
		"  render-timeout[:min_ids=<n>][:count=<n>]",
		"  render-500[:count=<n>]",
		"  null-render:<node-id>[:count=<n>]",
		"  status:<code>:<path-prefix>[:count=<n>]",
		"  plan-limited:variables[:count=<n>]",
		"  cdn-expired[:<path-prefix>][:count=<n>]",
	)
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, props{
		"service":    "mockfigma",
		"file_key":   FileKey,
		"branch_key": BranchKey,
		"routes":     Routes(),
	})
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.Stats())
}

func (s *Server) handleReset(w http.ResponseWriter, r *http.Request) {
	s.Reset()
	writeJSON(w, http.StatusOK, props{"reset": true})
}

func (s *Server) handleGetFaults(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.faults.describe())
}

func (s *Server) handleSetFaults(w http.ResponseWriter, r *http.Request) {
	spec := r.URL.Query().Get("rules")
	if err := s.faults.arm(spec); err != nil {
		writeJSON(w, http.StatusBadRequest, props{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, s.faults.describe())
}

func (s *Server) handleClearFaults(w http.ResponseWriter, r *http.Request) {
	s.faults.clear()
	writeJSON(w, http.StatusOK, s.faults.describe())
}

func (s *Server) handleBump(w http.ResponseWriter, r *http.Request) {
	key := controlKey(r)
	version, ok := s.BumpVersion(key)
	if !ok {
		writeError(w, errMessage, http.StatusNotFound, "Not found")
		return
	}
	s.writeNewVersion(w, key, version)
}

// handleEdit takes a JSON list of Edit objects; unknown fields are rejected
// so a misspelt name fails loudly instead of being ignored.
func (s *Server) handleEdit(w http.ResponseWriter, r *http.Request) {
	key := controlKey(r)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	var edits []Edit
	if err := dec.Decode(&edits); err != nil {
		writeJSON(w, http.StatusBadRequest, props{"error": "body must be a JSON list of edits: " + err.Error()})
		return
	}
	version, err := s.Edit(key, edits...)
	if errors.Is(err, errUnknownFile) {
		writeError(w, errMessage, http.StatusNotFound, "Not found")
		return
	}
	if err != nil {
		writeJSON(w, http.StatusBadRequest, props{"error": err.Error()})
		return
	}
	s.writeNewVersion(w, key, version)
}

func controlKey(r *http.Request) string {
	if key := r.URL.Query().Get("key"); key != "" {
		return key
	}
	return FileKey
}

func (s *Server) writeNewVersion(w http.ResponseWriter, key, version string) {
	f, _ := s.snapshot(key)
	writeJSON(w, http.StatusOK, props{"key": key, "version": version, "lastModified": timestamp(f.current().createdAt)})
}

func itoa(n int) string { return strconv.Itoa(n) }
