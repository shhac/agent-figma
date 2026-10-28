package fetch

import "time"

// renderURLLife is how long a render URL is trusted: Figma keeps them for 30 days.
const renderURLLife = 29 * 24 * time.Hour

type renderEntry struct {
	URL       string    `json:"url,omitempty"`
	Null      bool      `json:"null,omitempty"`
	FetchedAt time.Time `json:"fetched_at"`
}

type renderCache struct {
	Entries map[string]renderEntry `json:"entries"`
}

// renderView is a version's cached render URLs for one set of render options.
type renderView struct {
	entries map[string]renderEntry
	opt     string
	now     time.Time
}

func (v renderView) key(id string) string { return id + "|" + v.opt }

// usable returns a cached entry that has not outlived Figma's URL lifetime.
func (v renderView) usable(id string) (renderEntry, bool) {
	e, ok := v.entries[v.key(id)]
	return e, ok && v.now.Sub(e.FetchedAt) < renderURLLife
}

func (v renderView) missing(ids []string) []string {
	var out []string
	for _, id := range ids {
		if _, ok := v.usable(id); !ok {
			out = append(out, id)
		}
	}
	return out
}

func (s *Session) renderView(version string, req RenderRequest) renderView {
	var rc renderCache
	_ = s.File.JSONMap(cacheVersion(version), "renders").Load(&rc)
	if rc.Entries == nil {
		rc.Entries = map[string]renderEntry{}
	}
	return renderView{entries: rc.Entries, opt: req.optionKey(), now: s.now()}
}

// updateRenders applies change to the stored URLs under the store's lock, so
// parallel processes' additions and removals both survive, and refreshes the
// view from the result. If the cache cannot be written, the view is still
// updated so this command proceeds.
func (s *Session) updateRenders(version string, view *renderView, change func(map[string]renderEntry)) {
	var rc renderCache
	err := s.File.JSONMap(cacheVersion(version), "renders").Update(&rc, func() error {
		if rc.Entries == nil {
			rc.Entries = map[string]renderEntry{}
		}
		change(rc.Entries)
		return nil
	})
	if err != nil {
		change(view.entries)
		return
	}
	view.entries = rc.Entries
}
