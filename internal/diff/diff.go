// Package diff compares a node subtree between two cached file versions, as
// an agent would want to hear it: what was added, removed, moved, or changed,
// in the designer's terms (text, fills, spacing, components), grouped by the
// frame it is in.
package diff

import (
	"cmp"
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
	Label       string           `json:"label,omitempty"`
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
	records map[string]map[string]any
	order   []string
}

// load simplifies the subtrees with every style value inline: hoisting
// depends on what else is in the output, so a value used once in one version
// and twice in the other would otherwise look like a change.
func load(store *cache.NodeStore, roots []string) side {
	res := simplify.Simplify(store, roots, simplify.Options{View: simplify.ViewAll, MaxDepth: cache.Full, InlineStyles: true})
	s := side{records: map[string]map[string]any{}}
	for _, rec := range res.Records {
		raw, _ := json.Marshal(rec)
		var m map[string]any
		_ = json.Unmarshal(raw, &m)
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
		c.Label = str(s.records[c.ID]["label"])
		changes = append(changes, c)
	}
	for _, id := range cur.order {
		rec := cur.records[id]
		prev, existed := old.records[id]
		if !existed {
			if !insideWholeSubtree(id, cur, old, rootSet) {
				c := newChange(Added, id, rec)
				c.Descendants = descendants(cur, old, id)
				add(c, cur)
			}
			continue
		}
		if !rootSet[id] && str(prev["parent"]) != str(rec["parent"]) {
			c := newChange(Moved, id, rec)
			c.Fields = map[string]Delta{"parent": {From: prev["parent"], To: rec["parent"]}}
			add(c, cur)
		}
		if fields := fieldDeltas(prev, rec); len(fields) > 0 {
			c := newChange(Changed, id, rec)
			c.Fields = fields
			add(c, cur)
		}
	}
	for _, id := range old.order {
		if _, still := cur.records[id]; still || insideWholeSubtree(id, old, cur, rootSet) {
			continue
		}
		c := newChange(Removed, id, old.records[id])
		c.Descendants = descendants(old, cur, id)
		add(c, old)
	}
	return changes, summarise(changes, cur, old)
}

func newChange(kind, id string, rec map[string]any) Change {
	return Change{Change: kind, ID: id, Type: str(rec["type"]), Name: str(rec["name"])}
}

func fieldDeltas(prev, rec map[string]any) map[string]Delta {
	fields := map[string]Delta{}
	for _, f := range compared {
		if !reflect.DeepEqual(prev[f], rec[f]) {
			fields[f] = Delta{From: prev[f], To: rec[f]}
		}
	}
	return fields
}

// insideWholeSubtree reports a node whose parent appears (or disappears)
// along with it, so it is counted under that parent's record rather than
// listed on its own.
func insideWholeSubtree(id string, here, there side, roots map[string]bool) bool {
	parent := str(here.records[id]["parent"])
	return !roots[id] && here.records[parent] != nil && there.records[parent] == nil
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
			if name := cmp.Or(str(rec["label"]), str(rec["name"])); name != "" {
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

// descendants counts the nodes under id on one side that are absent from the
// other: what was added or removed along with id. A child moved out before
// its parent was removed is reported as moved, not counted here.
func descendants(here, there side, id string) int {
	n := 0
	for child, rec := range here.records {
		if _, elsewhere := there.records[child]; elsewhere {
			continue
		}
		for p := str(rec["parent"]); p != "" && here.records[p] != nil; p = str(here.records[p]["parent"]) {
			if p == id {
				n++
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
		name := cmp.Or(str(cur.records[id]["label"]), str(cur.records[id]["name"]), str(old.records[id]["label"]), str(old.records[id]["name"]))
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
