package mockfigma

import (
	"strconv"
	"sync"
	"time"

	"github.com/shhac/agent-figma/internal/ratelimit"
)

type versionEntry struct {
	id          string
	createdAt   time.Time
	label       any
	description any
}

// fileState is one fetchable key: the main file or its branch. Both serve the
// same document; they differ in name, version history, and which endpoints
// accept them.
type fileState struct {
	key      string
	name     string
	mainKey  string
	versions []versionEntry // newest first
}

func (f *fileState) current() versionEntry { return f.versions[0] }

func (f *fileState) bump() versionEntry {
	head := f.current()
	n, _ := strconv.ParseUint(head.id, 10, 64)
	next := versionEntry{id: strconv.FormatUint(n+1, 10), createdAt: head.createdAt.Add(time.Minute)}
	f.versions = append([]versionEntry{next}, f.versions...)
	return next
}

func (f *fileState) clone() fileState {
	out := *f
	out.versions = append([]versionEntry(nil), f.versions...)
	return out
}

func at(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func initialFiles() map[string]*fileState {
	return map[string]*fileState{
		FileKey: {
			key:  FileKey,
			name: FileName,
			versions: []versionEntry{
				{InitialVersion, at("2026-01-15T10:00:00Z"), "Checkout v2", "Adds the card and price row."},
				{"1000000000000000000", at("2026-01-14T16:30:00Z"), nil, nil},
				{"999999999999999999", at("2026-01-12T12:00:00Z"), "Button variants", "State variants for Button."},
				{"999999999999999998", at("2026-01-10T09:45:00Z"), nil, nil},
				{"999999999999999997", at("2026-01-08T14:00:00Z"), "Initial layout", nil},
			},
		},
		BranchKey: {
			key:     BranchKey,
			name:    BranchName,
			mainKey: FileKey,
			versions: []versionEntry{
				{InitialBranchVersion, at("2026-01-16T08:00:00Z"), nil, nil},
				{"1000000000000000100", at("2026-01-15T11:00:00Z"), "Branched", nil},
			},
		},
	}
}

// Stats is what GET /_mock/stats reports: Figma API calls (paths under /v1
// and /v2) by tier and path, in arrival order. Mock controls and CDN
// downloads are not counted.
type Stats struct {
	Tier1    int            `json:"tier1"`
	Tier2    int            `json:"tier2"`
	Tier3    int            `json:"tier3"`
	ByPath   map[string]int `json:"by_path"`
	Requests []Request      `json:"requests"`
}

type Request struct {
	Path  string `json:"path"`
	Query string `json:"query"`
}

type statsRecorder struct {
	mu    sync.Mutex
	stats Stats
}

func newStatsRecorder() *statsRecorder {
	r := &statsRecorder{}
	r.reset()
	return r
}

func (s *statsRecorder) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stats = Stats{ByPath: map[string]int{}, Requests: []Request{}}
}

func (s *statsRecorder) record(path, query string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch ratelimit.TierFor(path) {
	case ratelimit.Tier1:
		s.stats.Tier1++
	case ratelimit.Tier2:
		s.stats.Tier2++
	case ratelimit.Tier3:
		s.stats.Tier3++
	}
	s.stats.ByPath[path]++
	s.stats.Requests = append(s.stats.Requests, Request{Path: path, Query: query})
}

func (s *statsRecorder) snapshot() Stats {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := Stats{
		Tier1: s.stats.Tier1, Tier2: s.stats.Tier2, Tier3: s.stats.Tier3,
		ByPath:   make(map[string]int, len(s.stats.ByPath)),
		Requests: append([]Request{}, s.stats.Requests...),
	}
	for k, v := range s.stats.ByPath {
		out.ByPath[k] = v
	}
	return out
}
