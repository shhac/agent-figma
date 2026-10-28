package mockfigma

// props is one node's (or paint's, or style's) JSON object as Figma spells it.
type props = map[string]any

// node is one layer of the fixture document. Rendering copies props into a
// fresh map, so the fixture itself is never mutated by a request.
type node struct {
	id, typ, name string
	props         props
	// geometry holds fillGeometry/strokeGeometry, which Figma only returns
	// with geometry=paths.
	geometry  props
	children  []*node
	parent    *node
	container bool
}

var containerTypes = map[string]bool{
	"DOCUMENT":          true,
	"CANVAS":            true,
	"FRAME":             true,
	"GROUP":             true,
	"SECTION":           true,
	"COMPONENT":         true,
	"COMPONENT_SET":     true,
	"INSTANCE":          true,
	"BOOLEAN_OPERATION": true,
}

func el(typ, id, name string, p props, kids ...*node) *node {
	if p == nil {
		p = props{}
	}
	n := &node{id: id, typ: typ, name: name, props: p, children: kids, container: containerTypes[typ]}
	for _, k := range kids {
		k.parent = n
	}
	return n
}

func (n *node) hidden() bool {
	visible, ok := n.props["visible"].(bool)
	return ok && !visible
}

// hiddenInTree reports whether the node or any ancestor is invisible, which is
// what makes Figma return a null render for it.
func (n *node) hiddenInTree() bool {
	for p := n; p != nil; p = p.parent {
		if p.hidden() {
			return true
		}
	}
	return false
}

func (n *node) page() *node {
	for p := n; p != nil; p = p.parent {
		if p.typ == "CANVAS" {
			return p
		}
	}
	return nil
}

func (n *node) walk(fn func(*node)) {
	fn(n)
	for _, k := range n.children {
		k.walk(fn)
	}
}

type tree struct {
	root *node
	byID map[string]*node
}

func newTree(root *node) *tree {
	t := &tree{root: root, byID: map[string]*node{}}
	root.walk(func(n *node) { t.byID[n.id] = n })
	return t
}

type renderOpts struct {
	geometry bool
}

// collector records every node a response carries, in order: the source for
// the response's components/componentSets/styles maps and for the node count
// that the too-large fault compares against.
type collector struct {
	nodes []*node
}

func (n *node) own(opts renderOpts, col *collector) props {
	col.nodes = append(col.nodes, n)
	out := make(props, len(n.props)+len(n.geometry)+3)
	for k, v := range n.props {
		out[k] = v
	}
	if opts.geometry {
		for k, v := range n.geometry {
			out[k] = v
		}
	}
	out["id"] = n.id
	out["name"] = n.name
	out["type"] = n.typ
	return out
}

// subtree renders n and up to depth levels below it; a negative depth means
// the whole subtree. A container cut by the depth limit carries no children
// key at all, so a client can tell "not fetched" from "empty".
func subtree(n *node, depth int, opts renderOpts, col *collector) props {
	out := n.own(opts, col)
	if !n.container || depth == 0 {
		return out
	}
	kids := make([]any, 0, len(n.children))
	for _, k := range n.children {
		kids = append(kids, subtree(k, depth-1, opts, col))
	}
	out["children"] = kids
	return out
}

// selection is what GET /v1/files/:key?ids= returns: the listed nodes' full
// subtrees (plus the local main components their instances depend on), and
// the ancestor chain from the root down to each of them.
type selection struct {
	full map[*node]bool
	path map[*node]bool
}

func (t *tree) selectIDs(ids []string) selection {
	full := map[*node]bool{}
	queue := []*node{}
	for _, id := range ids {
		if n := t.byID[id]; n != nil {
			queue = append(queue, n)
		}
	}
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		if full[n] {
			continue
		}
		full[n] = true
		n.walk(func(d *node) {
			if d.typ != "INSTANCE" {
				return
			}
			componentID, _ := d.props["componentId"].(string)
			if main := t.byID[componentID]; main != nil {
				queue = append(queue, main)
			}
		})
	}
	path := map[*node]bool{}
	for n := range full {
		for p := n.parent; p != nil; p = p.parent {
			path[p] = true
		}
	}
	return selection{full: full, path: path}
}

// renderSelection renders the document for an ids request. Ancestors carry
// only the children on a path; every top-level CANVAS is present regardless
// (Figma's documented quirk), with an empty children list when off-path.
// depth counts from the document root, as it does on this endpoint.
func (t *tree) renderSelection(sel selection, depth int, opts renderOpts, col *collector) props {
	return selective(t.root, 0, sel, depth, opts, col)
}

func selective(n *node, level int, sel selection, depth int, opts renderOpts, col *collector) props {
	if sel.full[n] {
		return subtree(n, remaining(depth, level), opts, col)
	}
	out := n.own(opts, col)
	if depth >= 0 && level >= depth {
		return out
	}
	kids := []any{}
	for _, k := range n.children {
		if sel.full[k] || sel.path[k] || n.typ == "DOCUMENT" {
			kids = append(kids, selective(k, level+1, sel, depth, opts, col))
		}
	}
	out["children"] = kids
	return out
}

func remaining(depth, level int) int {
	if depth < 0 {
		return -1
	}
	return depth - level
}
