package fetch

import (
	"github.com/shhac/agent-figma/internal/cache"
	"github.com/shhac/agent-figma/internal/simplify"
)

// Cost tells an agent what following a reference would spend.
const (
	CostCached   = "cached"
	CostDownload = "download"
	CostTier1    = "tier1"
	CostTier2    = "tier2"
	CostTier3    = "tier3"
)

// Related is one @related line.
type Related struct {
	Rel    string         `json:"rel"`
	ID     string         `json:"id"`
	Of     string         `json:"of,omitempty"`
	Name   string         `json:"name,omitempty"`
	Type   string         `json:"type,omitempty"`
	Props  map[string]any `json:"props,omitempty"`
	Render string         `json:"render,omitempty"`
	Cost   string         `json:"cost"`
}

const maxRelated = 40

// RelatedTo lists what the cache knows around the requested nodes: their
// ancestors up to the page, the component set and sibling variants of a
// variant, and the main component behind an instance, each with what it
// would cost to read.
func RelatedTo(store *cache.NodeStore, ids []string) []Related {
	var out []Related
	seen := map[string]bool{}
	for _, id := range ids {
		seen[id] = true
	}
	cost := func(id string) string {
		if store.Has(id, cache.Full) {
			return CostCached
		}
		return CostTier1
	}
	add := func(r Related) {
		if len(out) >= maxRelated || seen[r.ID] {
			return
		}
		seen[r.ID] = true
		out = append(out, r)
	}
	describe := func(rel, of, id string) Related {
		r := Related{Rel: rel, ID: id, Of: of, Cost: cost(id)}
		if n, ok := store.Node(id); ok {
			r.Name, r.Type = n.Name, n.Type
		}
		return r
	}
	for _, id := range ids {
		stored := store.Nodes[id]
		if stored == nil {
			continue
		}
		node, _ := store.Node(id)
		if parent, ok := store.Node(stored.Parent); ok && parent.Type == "COMPONENT_SET" {
			add(describe("component_set", id, parent.ID))
			if ps := store.Nodes[parent.ID]; ps != nil {
				for _, sib := range ps.Children {
					r := describe("variant", id, sib)
					r.Props = variantPropsOf(store, sib)
					add(r)
				}
			}
		}
		for _, a := range store.Ancestors(id) {
			n, ok := store.Node(a)
			if !ok || n.Type == "DOCUMENT" {
				continue
			}
			rel := "ancestor"
			if n.Type == "CANVAS" {
				rel = "page"
			}
			r := describe(rel, id, a)
			// Ancestors are known only as far as the path to this node.
			if !store.Has(a, cache.Full) {
				r.Cost = CostTier1
			}
			add(r)
		}
		if node.Type == "INSTANCE" && node.ComponentID != "" {
			c, known := store.Components[node.ComponentID]
			r := describe("main_component", id, node.ComponentID)
			if known {
				r.Name = c.Name
				if c.Remote {
					r.Cost = CostTier1
				}
			}
			add(r)
			if known && c.ComponentSetID != "" {
				r := describe("component_set", id, c.ComponentSetID)
				if set, ok := store.ComponentSets[c.ComponentSetID]; ok {
					r.Name = set.Name
				}
				add(r)
			}
		}
	}
	return out
}

func variantPropsOf(store *cache.NodeStore, id string) map[string]any {
	res := simplify.Simplify(store, []string{id}, simplify.Options{View: simplify.ViewAll, MaxDepth: 0})
	if len(res.Records) == 0 {
		return nil
	}
	return res.Records[0].Props
}
