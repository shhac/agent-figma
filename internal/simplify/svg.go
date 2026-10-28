package simplify

import "slices"

var vectorTypes = []string{"VECTOR", "STAR", "LINE", "ELLIPSE", "POLYGON", "REGULAR_POLYGON", "RECTANGLE", "BOOLEAN_OPERATION"}

var svgContainers = []string{"FRAME", "GROUP", "INSTANCE", "BOOLEAN_OPERATION", "COMPONENT"}

// collapsesToSVG reports a container whose visible children are all vector
// shapes (or containers that collapse themselves), so it is one icon or
// illustration best exported as a single SVG. Auto-layout containers with
// fewer than 10 children are kept: those are usually real layout.
func (w *walker) collapsesToSVG(id string) bool {
	if w.svg == nil {
		w.svg = map[string]bool{}
	}
	if v, ok := w.svg[id]; ok {
		return v
	}
	w.svg[id] = false
	n, ok := w.node(id)
	if !ok || !slices.Contains(svgContainers, n.Type) || hasImageFill(n.Fills) {
		return false
	}
	stored := w.store.Nodes[id]
	if stored == nil || stored.KnownDepth == 0 {
		return false
	}
	children := w.visibleChildren(id)
	if len(children) == 0 {
		return false
	}
	if n.LayoutMode != "" && n.LayoutMode != "NONE" && len(children) < 10 {
		return false
	}
	for _, c := range children {
		cn, _ := w.node(c)
		if slices.Contains(vectorTypes, cn.Type) && !hasImageFill(cn.Fills) {
			continue
		}
		if w.collapsesToSVG(c) {
			continue
		}
		return false
	}
	w.svg[id] = true
	return true
}
