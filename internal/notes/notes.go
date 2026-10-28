// Package notes gathers the context designers leave around the layers being
// built: text and sticky notes on the canvas beside a frame, text in the
// section that holds it, comments pinned inside it, component descriptions,
// and Dev Mode annotations. Each note says where it came from, because some
// are designer-to-designer and some are written for whoever implements it.
package notes

import (
	"cmp"
	"math"
	"regexp"
	"slices"
	"strings"

	"github.com/shhac/agent-figma/internal/cache"
	"github.com/shhac/agent-figma/internal/figma"
)

// Sources.
const (
	SourceCanvas      = "canvas"
	SourceSection     = "section"
	SourceComment     = "comment"
	SourceDescription = "description"
	SourceAnnotation  = "annotation"
)

const DefaultRadius = 400

type Note struct {
	Source    string  `json:"source"`
	Text      string  `json:"text"`
	NodeID    string  `json:"node_id,omitempty"`
	Name      string  `json:"name,omitempty"`
	Position  string  `json:"position,omitempty"`
	Distance  float64 `json:"distance,omitempty"`
	Author    string  `json:"author,omitempty"`
	CreatedAt string  `json:"created_at,omitempty"`
	Resolved  bool    `json:"resolved,omitempty"`
	Replies   []Note  `json:"replies,omitempty"`
}

// Coverage says what could not be searched because it is not cached.
type Coverage struct {
	Complete bool `json:"complete"`
	// Uncached are ancestors whose other children are not known, so notes
	// beside the node there could not be looked for.
	Uncached []string `json:"uncached,omitempty"`
}

var noteName = regexp.MustCompile(`(?i)sticky|note|annotation|comment|todo`)

// Canvas finds notes beside id: note-like siblings of id and of each of its
// ancestors up to the page, within radius of id's box; and, inside a
// containing SECTION, its loose text at any distance.
func Canvas(store *cache.NodeStore, id string, radius float64) ([]Note, Coverage) {
	var cov Coverage
	target, ok := store.Node(id)
	if !ok || target.AbsoluteBoundingBox == nil {
		cov.Complete = true
		return nil, cov
	}
	var out []Note
	seen := map[string]bool{}
	child := id
	for _, ancestorID := range store.Ancestors(id) {
		ancestor, ok := store.Node(ancestorID)
		if !ok || ancestor.Type == "DOCUMENT" {
			break
		}
		stored := store.Nodes[ancestorID]
		inSection := ancestor.Type == "SECTION"
		// Inside a frame or group, siblings are the design itself; notes
		// sit on the page or in a section, so only those are searched.
		if !inSection && ancestor.Type != "CANVAS" {
			child = ancestorID
			continue
		}
		if stored.KnownDepth == 0 {
			cov.Uncached = append(cov.Uncached, ancestorID)
		}
		for _, sib := range stored.Children {
			if sib == child {
				continue
			}
			n, ok := store.Node(sib)
			if !ok || !n.IsVisible() || n.AbsoluteBoundingBox == nil {
				continue
			}
			text := noteText(store, sib, n, inSection)
			if text == "" || seen[text] {
				continue
			}
			distance, position := relate(*target.AbsoluteBoundingBox, *n.AbsoluteBoundingBox)
			note := Note{Text: text, NodeID: sib, Name: n.Name, Position: position, Distance: math.Round(distance)}
			switch {
			case inSection:
				note.Source = SourceSection
			case distance <= radius:
				note.Source = SourceCanvas
			default:
				continue
			}
			seen[text] = true
			out = append(out, note)
		}
		if ancestor.Type == "CANVAS" {
			break
		}
		child = ancestorID
	}
	slices.SortStableFunc(out, func(a, b Note) int { return int(a.Distance - b.Distance) })
	cov.Complete = len(cov.Uncached) == 0
	return out, cov
}

// noteText is the text a sibling contributes: a TEXT node's characters, or
// the texts inside a note-like container (a sticky, an annotation frame).
// Inside a section, only loose text counts: frames there are content.
func noteText(store *cache.NodeStore, id string, n figma.Node, inSection bool) string {
	if n.Type == "TEXT" {
		return strings.TrimSpace(n.Characters)
	}
	if inSection || !noteName.MatchString(n.Name) {
		return ""
	}
	var texts []string
	store.WalkVisible([]string{id}, func(_ string, d figma.Node) {
		if d.Type == "TEXT" {
			if t := strings.TrimSpace(d.Characters); t != "" {
				texts = append(texts, t)
			}
		}
	})
	return strings.Join(texts, "\n")
}

// relate is the gap between two boxes (0 when they overlap) and where b sits
// relative to a.
func relate(a, b figma.Rect) (float64, string) {
	dx := math.Max(0, math.Max(a.X-(b.X+b.Width), b.X-(a.X+a.Width)))
	dy := math.Max(0, math.Max(a.Y-(b.Y+b.Height), b.Y-(a.Y+a.Height)))
	switch {
	case dx == 0 && dy == 0:
		return 0, "overlapping"
	case dy >= dx && b.Y+b.Height <= a.Y:
		return math.Hypot(dx, dy), "above"
	case dy >= dx:
		return math.Hypot(dx, dy), "below"
	case b.X+b.Width <= a.X:
		return math.Hypot(dx, dy), "left"
	default:
		return math.Hypot(dx, dy), "right"
	}
}

// Descriptions are the descriptions of components used under id.
func Descriptions(store *cache.NodeStore, id string) []Note {
	var out []Note
	seen := map[string]bool{}
	store.WalkVisible([]string{id}, func(nid string, n figma.Node) {
		if n.Type != "INSTANCE" || n.ComponentID == "" {
			return
		}
		c := store.Components[n.ComponentID]
		name, desc := c.Name, c.Description
		if set, ok := store.ComponentSets[c.ComponentSetID]; ok && set.Description != "" {
			name, desc = set.Name, set.Description
		}
		if desc = strings.TrimSpace(desc); desc == "" || seen[name] || isKeywordList(desc) {
			return
		}
		seen[name] = true
		out = append(out, Note{Source: SourceDescription, Text: desc, NodeID: nid, Name: name})
	})
	return out
}

// isKeywordList reports a description that is search keywords ("Close,
// Cancel, X"), as icon libraries use, rather than guidance.
func isKeywordList(desc string) bool {
	parts := strings.Split(desc, ",")
	if len(parts) < 2 {
		return false
	}
	for _, p := range parts {
		if len(strings.Fields(p)) > 2 {
			return false
		}
	}
	return true
}

// Annotations are Dev Mode annotations on id and its descendants.
func Annotations(store *cache.NodeStore, id string) []Note {
	var out []Note
	store.WalkVisible([]string{id}, func(nid string, n figma.Node) {
		for _, a := range n.Annotations {
			if text := strings.TrimSpace(cmp.Or(a.LabelMarkdown, a.Label)); text != "" {
				out = append(out, Note{Source: SourceAnnotation, Text: text, NodeID: nid, Name: n.Name})
			}
		}
	})
	return out
}

// Comments are the threads pinned to id or a descendant, replies attached.
func Comments(store *cache.NodeStore, id string, all []figma.Comment) []Note {
	inside := map[string]bool{}
	store.WalkVisible([]string{id}, func(nid string, _ figma.Node) { inside[nid] = true })
	threads := map[string]*Note{}
	var order []string
	for _, c := range all {
		if c.ParentID != "" || !inside[c.ClientMeta.NodeID] {
			continue
		}
		n := commentNote(c)
		n.NodeID = c.ClientMeta.NodeID
		threads[c.ID] = &n
		order = append(order, c.ID)
	}
	for _, c := range all {
		if t := threads[c.ParentID]; t != nil {
			t.Replies = append(t.Replies, commentNote(c))
		}
	}
	out := make([]Note, 0, len(order))
	for _, id := range order {
		out = append(out, *threads[id])
	}
	return out
}

func commentNote(c figma.Comment) Note {
	return Note{Source: SourceComment, Text: strings.TrimSpace(c.Message), Author: c.User.Handle, CreatedAt: c.CreatedAt, Resolved: c.ResolvedAt != ""}
}
