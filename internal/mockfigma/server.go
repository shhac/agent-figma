// Package mockfigma is a local stand-in for the Figma REST API: one synthetic
// file (and a branch of it) with realistic node shapes, a fake CDN for render
// and fill URLs, per-tier request counters, and fault injection. It backs the
// CLI contract tests and `make mock`. Every identifier is synthetic.
package mockfigma

import (
	"net/http"
	"strings"
	"sync"
)

type Server struct {
	mux *http.ServeMux
	// fixture is the document as built: every version's starting content and
	// what the published-library endpoints describe, which edits never touch.
	fixture *tree
	stats   *statsRecorder
	faults  *faultSet

	mu    sync.Mutex
	files map[string]*fileState
}

func New() *Server {
	fixture := newTree(buildDocument())
	s := &Server{
		mux:     http.NewServeMux(),
		fixture: fixture,
		stats:   newStatsRecorder(),
		faults:  newFaultSet(),
		files:   initialFiles(fixture),
	}
	s.routes()
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if isAPIPath(r.URL.Path) {
		s.stats.record(r.URL.Path, r.URL.RawQuery)
	}
	s.mux.ServeHTTP(w, r)
}

func isAPIPath(path string) bool {
	return strings.HasPrefix(path, "/v1/") || strings.HasPrefix(path, "/v2/")
}

// Stats returns the API calls seen since New or the last Reset.
func (s *Server) Stats() Stats { return s.stats.snapshot() }

// Reset clears counters and faults and restores both files' original
// version histories, discarding every edit.
func (s *Server) Reset() {
	s.stats.reset()
	s.faults.clear()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.files = initialFiles(s.fixture)
}

// SetFaults arms standing fault rules (see the grammar in faults.go);
// an empty spec clears them.
func (s *Server) SetFaults(spec string) error { return s.faults.arm(spec) }

// BumpVersion simulates an edit that changes nothing visible: a new newest
// version of the file behind key, serving the same content. It returns the
// new version.
func (s *Server) BumpVersion(key string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.files[key]
	if !ok {
		return "", false
	}
	return f.bump(f.current().tree).id, true
}

func (s *Server) snapshot(key string) (fileState, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.files[key]
	if !ok {
		return fileState{}, false
	}
	return f.clone(), true
}

// api wraps an endpoint with what every Figma call goes through: auth, then
// the transport-level faults (429, forced status, plan limits).
func (s *Server) api(style errorStyle, serve func(*call)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := &call{w: w, r: r, style: style, base: "http://" + r.Host}
		if !authorize(c) {
			return
		}
		rules, err := s.faults.forRequest(r)
		if err != nil {
			c.fail(http.StatusBadRequest, "Invalid X-Mock-Fault: "+err.Error())
			return
		}
		c.rules = rules
		if s.transportFault(c) {
			return
		}
		serve(c)
	}
}

func authorize(c *call) bool {
	token := c.r.Header.Get("X-Figma-Token")
	if bearer, ok := strings.CutPrefix(c.r.Header.Get("Authorization"), "Bearer "); ok && token == "" {
		token = strings.TrimSpace(bearer)
	}
	if token == "" || token == ExpiredToken {
		c.fail(http.StatusForbidden, "Invalid token")
		return false
	}
	return true
}

func (s *Server) transportFault(c *call) bool {
	path := c.r.URL.Path
	f := s.faults.take(c.rules, func(f *fault) bool {
		if f.kind == faultPlanLimited {
			return isVariablesPath(path)
		}
		return f.matchesPath(path)
	}, faultRateLimit, faultStatus, faultPlanLimited)
	if f == nil {
		return false
	}
	switch f.kind {
	case faultRateLimit:
		h := c.w.Header()
		h.Set("Retry-After", itoa(f.retry))
		h.Set("X-Figma-Plan-Tier", f.plan)
		h.Set("X-Figma-Rate-Limit-Type", f.limitType)
		h.Set("X-Figma-Upgrade-Link", upgradeLink(f.limitType))
		c.fail(http.StatusTooManyRequests, "Rate limit exceeded")
	case faultPlanLimited:
		c.fail(http.StatusForbidden, "Limited by Figma plan")
	default:
		c.fail(f.status, http.StatusText(f.status))
	}
	return true
}

func upgradeLink(limitType string) string {
	if limitType == "low" {
		return "https://www.figma.com/settings"
	}
	return "https://www.figma.com/pricing"
}

func isVariablesPath(path string) bool {
	return strings.HasPrefix(path, "/v1/files/") && strings.Contains(path, "/variables/")
}

// file resolves the {key} path value. Library and dev-resource endpoints need
// the main file key and answer a branch key with 404, as Figma does.
func (s *Server) file(c *call, allowBranch bool) (fileState, bool) {
	f, ok := s.snapshot(c.r.PathValue("key"))
	if !ok || (!allowBranch && f.mainKey != "") {
		c.notFound()
		return fileState{}, false
	}
	return f, true
}

// tooLarge applies a too-large rule to a response carrying nodes nodes.
func (s *Server) tooLarge(c *call, nodes int) bool {
	f := s.faults.take(c.rules, func(f *fault) bool {
		return strings.HasPrefix(c.r.URL.Path, f.target) && nodes >= f.minNodes
	}, faultTooLarge)
	if f == nil {
		return false
	}
	c.fail(http.StatusBadRequest, "Request too large. If applicable, filter by query params.")
	return true
}
