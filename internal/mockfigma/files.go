package mockfigma

import (
	"net/http"
	"net/url"
	"strconv"
	"time"
)

func timestamp(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func (s *Server) getMe(c *call) {
	me := viewer(c.base)
	me["email"] = "example.user@example.com"
	c.ok(me)
}

// fileVersion is the version a read is served at, content and all: the
// ?version= named (404 when the history lacks it), otherwise the newest.
func fileVersion(c *call, f fileState) (versionEntry, bool) {
	want := c.r.URL.Query().Get("version")
	if want == "" {
		return f.current(), true
	}
	v, ok := f.version(want)
	if !ok {
		c.notFound()
	}
	return v, ok
}

// depthParam reads ?depth=, which Figma documents as a positive integer;
// -1 means unlimited.
func depthParam(c *call) (int, bool) {
	raw := c.r.URL.Query().Get("depth")
	if raw == "" {
		return -1, true
	}
	depth, err := strconv.Atoi(raw)
	if err != nil || depth < 1 {
		c.fail(http.StatusBadRequest, "Invalid parameter: depth must be a positive integer")
		return 0, false
	}
	return depth, true
}

func (s *Server) fileHeader(c *call, f fileState, v versionEntry) props {
	return props{
		"name":         f.name,
		"role":         "owner",
		"lastModified": timestamp(f.current().createdAt),
		"editorType":   "figma",
		"thumbnailUrl": c.base + "/_cdn/thumbnail/" + f.key + ".png",
		"version":      v.id,
	}
}

func (s *Server) getFile(c *call) {
	f, ok := s.file(c, true)
	if !ok {
		return
	}
	v, ok := fileVersion(c, f)
	if !ok {
		return
	}
	depth, ok := depthParam(c)
	if !ok {
		return
	}
	q := c.r.URL.Query()
	opts := renderOpts{geometry: q.Get("geometry") == "paths"}
	col := &collector{}
	var document props
	if ids := splitList(q.Get("ids")); len(ids) > 0 {
		document = v.tree.renderSelection(v.tree.selectIDs(ids), depth, opts, col)
	} else {
		document = subtree(v.tree.root, depth, opts, col)
	}
	if s.tooLarge(c, len(col.nodes)) {
		return
	}
	body := s.fileHeader(c, f, v)
	components, componentSets, styles := dependencies(col.nodes)
	body["document"] = document
	body["components"] = components
	body["componentSets"] = componentSets
	body["styles"] = styles
	body["schemaVersion"] = 0
	body["linkAccess"] = "view"
	if q.Get("branch_data") == "true" {
		s.addBranchData(c, f, body)
	}
	c.ok(body)
}

func (s *Server) addBranchData(c *call, f fileState, body props) {
	if f.mainKey != "" {
		body["mainFileKey"] = f.mainKey
		return
	}
	branch, _ := s.snapshot(BranchKey)
	body["branches"] = []any{props{
		"key":           branch.key,
		"name":          branch.name,
		"thumbnail_url": c.base + "/_cdn/thumbnail/" + branch.key + ".png",
		"last_modified": timestamp(branch.current().createdAt),
	}}
}

func (s *Server) getNodes(c *call) {
	f, ok := s.file(c, true)
	if !ok {
		return
	}
	v, ok := fileVersion(c, f)
	if !ok {
		return
	}
	depth, ok := depthParam(c)
	if !ok {
		return
	}
	q := c.r.URL.Query()
	ids := splitList(q.Get("ids"))
	if len(ids) == 0 {
		c.fail(http.StatusBadRequest, "Missing parameter: ids")
		return
	}
	opts := renderOpts{geometry: q.Get("geometry") == "paths"}
	nodes := props{}
	total := 0
	for _, id := range ids {
		n := v.tree.byID[id]
		if n == nil {
			nodes[id] = nil
			continue
		}
		col := &collector{}
		document := subtree(n, depth, opts, col)
		total += len(col.nodes)
		components, componentSets, styles := dependencies(col.nodes)
		nodes[id] = props{
			"document":      document,
			"components":    components,
			"componentSets": componentSets,
			"styles":        styles,
			"schemaVersion": 0,
		}
	}
	if s.tooLarge(c, total) {
		return
	}
	body := s.fileHeader(c, f, v)
	body["nodes"] = nodes
	c.ok(body)
}

func (s *Server) getMeta(c *call) {
	f, ok := s.file(c, true)
	if !ok {
		return
	}
	link := "https://www.figma.com/design/" + FileKey + "/Example-File"
	if f.mainKey != "" {
		link = "https://www.figma.com/design/" + FileKey + "/branch/" + f.key + "/Example-File"
	}
	c.ok(props{"file": props{
		"name":            f.name,
		"folder_name":     "Example Project",
		"last_touched_at": timestamp(f.current().createdAt),
		"creator":         designer(c.base),
		"last_touched_by": designer(c.base),
		"thumbnail_url":   c.base + "/_cdn/thumbnail/" + f.key + ".png",
		"editorType":      "figma",
		"version":         f.current().id,
		"role":            "owner",
		"link_access":     "view",
		"url":             link,
	}})
}

// getVersions pages newest-first. before=<id> lists older versions, after=<id>
// newer ones (the page_size closest to it); next_page and prev_page are full
// URLs that continue in either direction.
func (s *Server) getVersions(c *call) {
	f, ok := s.file(c, true)
	if !ok {
		return
	}
	q := c.r.URL.Query()
	pageSize := 30
	if raw := q.Get("page_size"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 50 {
			c.fail(http.StatusBadRequest, "Invalid parameter: page_size must be between 1 and 50")
			return
		}
		pageSize = n
	}
	before, okBefore := versionCursor(c, "before")
	after, okAfter := versionCursor(c, "after")
	if !okBefore || !okAfter {
		return
	}
	window := []versionEntry{}
	for _, v := range f.versions {
		id, _ := strconv.ParseUint(v.id, 10, 64)
		if (before == 0 || id < before) && (after == 0 || id > after) {
			window = append(window, v)
		}
	}
	start, end := 0, min(pageSize, len(window))
	if after != 0 && before == 0 {
		start, end = max(0, len(window)-pageSize), len(window)
	}
	page := window[start:end]
	versions := make([]any, 0, len(page))
	for _, v := range page {
		versions = append(versions, props{
			"id":            v.id,
			"created_at":    timestamp(v.createdAt),
			"label":         v.label,
			"description":   v.description,
			"user":          designer(c.base),
			"thumbnail_url": c.base + "/_cdn/thumbnail/" + f.key + "-" + v.id + ".png",
		})
	}
	pagination := props{}
	if len(page) > 0 {
		pageURL := func(param, id string) string {
			values := url.Values{"page_size": {strconv.Itoa(pageSize)}, param: {id}}
			return c.base + c.r.URL.Path + "?" + values.Encode()
		}
		if newest := page[0]; newest.id != f.versions[0].id {
			pagination["prev_page"] = pageURL("after", newest.id)
		}
		if oldest := page[len(page)-1]; oldest.id != f.versions[len(f.versions)-1].id {
			pagination["next_page"] = pageURL("before", oldest.id)
		}
	}
	c.ok(props{"versions": versions, "pagination": pagination})
}

func versionCursor(c *call, name string) (uint64, bool) {
	raw := c.r.URL.Query().Get(name)
	if raw == "" {
		return 0, true
	}
	n, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		c.fail(http.StatusBadRequest, "Invalid parameter: "+name)
		return 0, false
	}
	return n, true
}

func (s *Server) getComments(c *call) {
	f, ok := s.file(c, true)
	if !ok {
		return
	}
	c.ok(props{"comments": fixtureComments(f.key, c.base)})
}
