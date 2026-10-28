// Package diff compares a node subtree between two cached file versions, as
// an agent would want to hear it: what was added, removed, moved, or changed,
// in the designer's terms (text, fills, spacing, components), grouped by the
// frame it is in.
package diff

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"

	"github.com/shhac/agent-figma/internal/cache"
	"github.com/shhac/agent-figma/internal/simplify"
)

// Change kinds.
const (
	Added   = "added"
	Removed = "removed"
	Changed = "changed"
	Moved   = "moved"
)

// Change is one record of the diff.
type Change struct {
	Change      string           `json:"change"`
	ID          string           `json:"id"`
	Type        string           `json:"type"`
	Name        string           `json:"name,omitempty"`
	Path        string           `json:"path,omitempty"`
	Fields      map[string]Delta `json:"fields,omitempty"`
	Descendants int              `json:"descendants,omitempty"`
	frame       string
}

// Delta is one field's before and after.
type Delta struct {
	From any `json:"from"`
	To   any `json:"to"`
}

// Frame counts the changes inside one top-level frame of the diff.
type Frame struct {
	ID      string `json:"id"`
	Name    string `json:"name,omitempty"`
	Changes int    `json:"changes"`
}

// Summary is the @diff line.
type Summary struct {
	From    string  `json:"from"`
	To      string  `json:"to"`
	Added   int     `json:"added"`
	Removed int     `json:"removed"`
	Changed int     `json:"changed"`
	Moved   int     `json:"moved"`
	Frames  []Frame `json:"frames"`
}

// compared are the simplified fields a change is reported for.
var compared = []string{"name", "type", "text", "texts", "size", "pos", "layout", "fills", "strokes", "effects", "radius", "opacity", "component", "props", "text_style", "svg", "export"}

type side struct {
	store   *cache.NodeStore
	records map[string]map[string]any
	order   []string
}

func load(store *cache.NodeStore, roots []string) side {
	res := simplify.Simplify(store, roots, simplify.Options{View: simplify.ViewAll, MaxDepth: cache.Full})
	s := side{store: store, records: map[string]map[string]any{}}
	for _, rec := range res.Records {
		raw, _ := json.Marshal(rec)
		var m map[string]any
		_ = json.Unmarshal(raw, &m)
		for k, v := range m {
			if id, ok := v.(string); ok {
				if entry, ok := res.Styles[id]; ok {
					m[k] = entry.Value
				}
			}
		}
		s.records[rec.ID] = m
		s.order = append(s.order, rec.ID)
	}
	return s
}

// Compare diffs the subtrees under roots between two versions' stores. Both
// stores must hold those subtrees in full.
func Compare(before, after *cache.NodeStore, roots []string) ([]Change, Summary) {
	old, cur := load(before, roots), load(after, roots)
	rootSet := map[string]bool{}
	for _, r := range roots {
		rootSet[r] = true
	}
	var changes []Change
	add := func(c Change, s side) {
		c.Path, c.frame = path(s, c.ID, rootSet)
		changes = append(changes, c)
	}
	for _, id := range cur.order {
		rec := cur.records[id]
		prev, existed := old.records[id]
		if !existed {
			if parent := str(rec["parent"]); !rootSet[id] && cur.records[parent] != nil && old.records[parent] == nil {
				continue // inside a subtree that was added as a whole
			}
			add(Change{Change: Added, ID: id, Type: str(rec["type"]), Name: str(rec["name"]), Descendants: descendants(cur, id)}, cur)
			continue
		}
		fields := map[string]Delta{}
		for _, f := range compared {
			if !reflect.DeepEqual(prev[f], rec[f]) {
				fields[f] = Delta{From: prev[f], To: rec[f]}
			}
		}
		if !rootSet[id] && str(prev["parent"]) != str(rec["parent"]) {
			add(Change{Change: Moved, ID: id, Type: str(rec["type"]), Name: str(rec["name"]), Fields: map[string]Delta{"parent": {From: prev["parent"], To: rec["parent"]}}}, cur)
		}
		if len(fields) > 0 {
			add(Change{Change: Changed, ID: id, Type: str(rec["type"]), Name: str(rec["name"]), Fields: fields}, cur)
		}
	}
	for _, id := range old.order {
		if _, still := cur.records[id]; still {
			continue
		}
		rec := old.records[id]
		if parent := str(rec["parent"]); !rootSet[id] && old.records[parent] != nil && cur.records[parent] == nil {
			continue // inside a subtree that was removed as a whole
		}
		add(Change{Change: Removed, ID: id, Type: str(rec["type"]), Name: str(rec["name"]), Descendants: descendants(old, id)}, old)
	}
	return changes, summarise(changes, cur, old)
}

// path names the meaningful ancestors under the diff root, root first;
// auto-named wrappers (dropped from simplified names) are skipped. It also
// returns the ancestor directly under the root, the change's frame.
func path(s side, id string, roots map[string]bool) (string, string) {
	var names []string
	frame := id
	for cur := id; ; {
		rec := s.records[cur]
		if rec == nil {
			break
		}
		if cur != id {
			if name := str(rec["name"]); name != "" {
				names = append(names, name)
			}
		}
		if roots[cur] {
			break
		}
		parent := str(rec["parent"])
		if roots[parent] || s.records[parent] == nil {
			frame = cur
		}
		cur = parent
	}
	slices.Reverse(names)
	return strings.Join(names, " › "), frame
}

func descendants(s side, id string) int {
	n := 0
	for _, rec := range s.records {
		for p := str(rec["parent"]); p != ""; p = str(s.records[p]["parent"]) {
			if p == id {
				n++
				break
			}
			if s.records[p] == nil {
				break
			}
		}
	}
	return n
}

func summarise(changes []Change, cur, old side) Summary {
	var sum Summary
	counts := map[string]int{}
	var frames []string
	for _, c := range changes {
		switch c.Change {
		case Added:
			sum.Added++
		case Removed:
			sum.Removed++
		case Changed:
			sum.Changed++
		case Moved:
			sum.Moved++
		}
		if counts[c.frame] == 0 {
			frames = append(frames, c.frame)
		}
		counts[c.frame]++
	}
	sum.Frames = []Frame{}
	for _, id := range frames {
		name := str(cur.records[id]["name"])
		if name == "" {
			name = str(old.records[id]["name"])
		}
		sum.Frames = append(sum.Frames, Frame{ID: id, Name: name, Changes: counts[id]})
	}
	return sum
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

// FrameIDs are the frames containing changes, for rendering before/after.
func (s Summary) FrameIDs(limit int) []string {
	var ids []string
	for _, f := range s.Frames {
		if len(ids) == limit {
			break
		}
		ids = append(ids, f.ID)
	}
	return ids
}
