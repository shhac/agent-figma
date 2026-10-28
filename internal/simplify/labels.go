package simplify

import (
	"regexp"
	"slices"
	"strings"

	"github.com/shhac/agent-figma/internal/cache"
)

const labelRunes = 60

var labelledTypes = []string{"FRAME", "GROUP", "SECTION", "COMPONENT", "COMPONENT_SET", "INSTANCE"}

var genericNames = map[string]bool{
	"card": true, "container": true, "content": true, "contents": true, "wrapper": true, "frame": true,
	"group": true, "section": true, "box": true, "item": true, "row": true, "column": true, "col": true,
	"stack": true, "body": true, "layout": true, "block": true, "inner": true, "outer": true, "main": true,
}

var nonWord = regexp.MustCompile(`[^\p{L}]+`)

// IsGenericName reports a name that says nothing about what a node shows:
// empty, or a generic word once emoji, punctuation, and numbers are dropped.
func IsGenericName(name string) bool {
	word := strings.ToLower(strings.TrimSpace(nonWord.ReplaceAllString(name, " ")))
	return word == "" || genericNames[word]
}

// Labeler finds the most prominent heading under a node: the visible TEXT
// with the largest font size, the first in depth order among equals.
// Results are memoised per subtree, so labelling a whole page stays linear.
type Labeler struct {
	store *cache.NodeStore
	memo  map[string]heading
	// names counts container names in some output: a name three or more of
	// them share does not tell them apart, so they get labels too.
	names map[string]int
}

type heading struct {
	text string
	size float64
	ok   bool
}

func NewLabeler(store *cache.NodeStore) *Labeler {
	return &Labeler{store: store, memo: map[string]heading{}}
}

// CountNames records the container names among ids, so names they share
// count as generic.
func (l *Labeler) CountNames(ids []string) {
	if l.names == nil {
		l.names = map[string]int{}
	}
	for _, id := range ids {
		if n, ok := l.store.Node(id); ok && slices.Contains(labelledTypes, n.Type) && n.Name != "" {
			l.names[n.Name]++
		}
	}
}

// For returns the label for a container whose name is generic (or shared
// by three or more counted nodes), or "" when the node keeps its own name or
// has no text under it.
func (l *Labeler) For(id string) string {
	n, ok := l.store.Node(id)
	if !ok || !slices.Contains(labelledTypes, n.Type) {
		return ""
	}
	if !IsGenericName(displayName(n)) && l.names[n.Name] < 3 {
		return ""
	}
	if h := l.Heading(id); h != n.Name {
		return h
	}
	return ""
}

// Heading is the most prominent text under id, previewed.
func (l *Labeler) Heading(id string) string {
	if h := l.find(id); h.ok {
		return preview(h.text, labelRunes)
	}
	return ""
}

func (l *Labeler) find(id string) heading {
	if h, done := l.memo[id]; done {
		return h
	}
	l.memo[id] = heading{}
	n, ok := l.store.Node(id)
	if !ok || !n.IsVisible() {
		return heading{}
	}
	var best heading
	if n.Type == "TEXT" && strings.TrimSpace(n.Characters) != "" {
		best = heading{text: n.Characters, ok: true}
		if n.Style != nil {
			best.size = n.Style.FontSize
		}
	}
	if stored := l.store.Nodes[id]; stored != nil {
		for _, c := range stored.Children {
			if h := l.find(c); h.ok && (!best.ok || h.size > best.size) {
				best = h
			}
		}
	}
	l.memo[id] = best
	return best
}
