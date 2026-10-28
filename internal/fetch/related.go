package fetch

import "github.com/shhac/agent-figma/internal/cache"

// relatedIDs are uncached ids likely to be wanted alongside need, from what
// the cache already knows: the component set a variant belongs to (whose
// subtree holds every sibling variant), and the local main component and set
// behind an instance. setOf maps published variant node ids to their set,
// from the cached Tier 3 library, which knows sets before any Tier 1 call.
func relatedIDs(known *cache.NodeStore, need []string, setOf map[string]string, limit int) []string {
	var out []string
	seen := map[string]bool{}
	for _, id := range need {
		seen[id] = true
	}
	add := func(id string) {
		if id == "" || seen[id] || len(out) >= limit || known.Has(id, cache.Full) {
			return
		}
		seen[id] = true
		out = append(out, id)
	}
	for _, id := range need {
		add(setOf[id])
		n, ok := known.Node(id)
		if !ok {
			continue
		}
		if stored := known.Nodes[id]; stored != nil {
			if parent, ok := known.Node(stored.Parent); ok && parent.Type == "COMPONENT_SET" {
				add(parent.ID)
			}
		}
		if n.Type == "INSTANCE" && n.ComponentID != "" {
			if c, ok := known.Components[n.ComponentID]; ok && !c.Remote {
				add(n.ComponentID)
				add(c.ComponentSetID)
			}
		}
	}
	return out
}
