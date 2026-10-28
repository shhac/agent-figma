package mockfigma

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
)

// EditOp is the kind of change one Edit makes.
type EditOp string

const (
	// EditSet sets one top-level property of Node: Field names it, Value is
	// its new JSON value ("name" takes a string; "id", "type" and "children"
	// cannot be set). Setting absoluteBoundingBox on a vector-like node also
	// refreshes its geometry=paths output.
	EditSet EditOp = "set"
	// EditRemove deletes Node and its subtree.
	EditRemove EditOp = "remove"
	// EditAdd inserts NodeJSON (a Figma-shaped node object with id, type and
	// optionally name and children) as a child of Parent at Index.
	EditAdd EditOp = "add"
	// EditMove reparents Node under Parent at Index (counted after Node has
	// left its old parent).
	EditMove EditOp = "move"
)

// AppendIndex, or any Index past the end, puts an added or moved node last.
const AppendIndex = -1

// Edit is one change to a file's document. Server.Edit and POST /_mock/edit
// apply a list of them to the newest version as a single new version. JSON
// names are snake_case: {"op", "node", "field", "value", "parent", "index",
// "node_json"}.
type Edit struct {
	Op     EditOp `json:"op"`
	Node   string `json:"node,omitempty"`
	Field  string `json:"field,omitempty"`
	Value  any    `json:"value,omitempty"`
	Parent string `json:"parent,omitempty"`
	// Index is the position among Parent's children; negative or past the
	// end appends. JSON callers who omit it insert first.
	Index    int             `json:"index,omitempty"`
	NodeJSON json.RawMessage `json:"node_json,omitempty"`
}

var errUnknownFile = errors.New("unknown file key")

// Edit applies edits, in order, to the newest version of the file behind key
// and publishes the result as one new newest version (one higher and a minute
// later, like BumpVersion), returning its id. Older versions keep their
// content. Nothing is published if any edit fails: an unknown node id, an
// unknown op, a field that cannot be set, or an add or move that would break
// the tree.
func (s *Server) Edit(key string, edits ...Edit) (string, error) {
	if len(edits) == 0 {
		return "", errors.New("no edits given")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.files[key]
	if !ok {
		return "", fmt.Errorf("%w %q", errUnknownFile, key)
	}
	doc := f.current().tree.clone()
	for i, e := range edits {
		if err := doc.apply(e); err != nil {
			return "", fmt.Errorf("edit %d (%s): %w", i, e.Op, err)
		}
	}
	return f.bump(doc).id, nil
}

func (t *tree) apply(e Edit) error {
	switch e.Op {
	case EditSet:
		return t.set(e)
	case EditRemove:
		return t.remove(e)
	case EditAdd:
		return t.add(e)
	case EditMove:
		return t.move(e)
	}
	return fmt.Errorf("unknown op %q (want set, remove, add, or move)", e.Op)
}

func (t *tree) lookup(role, id string) (*node, error) {
	if n := t.byID[id]; n != nil {
		return n, nil
	}
	return nil, fmt.Errorf("unknown %s id %q", role, id)
}

func (t *tree) set(e Edit) error {
	n, err := t.lookup("node", e.Node)
	if err != nil {
		return err
	}
	value, err := canonicalJSON(e.Value)
	if err != nil {
		return err
	}
	switch e.Field {
	case "":
		return errors.New("set needs a field")
	case "id", "type", "children":
		return fmt.Errorf("field %q cannot be set", e.Field)
	case "name":
		name, ok := value.(string)
		if !ok {
			return errors.New("name must be a string")
		}
		n.name = name
		return nil
	}
	n.props[e.Field] = value
	if e.Field == "absoluteBoundingBox" {
		addGeometry(n)
	}
	return nil
}

func (t *tree) remove(e Edit) error {
	n, err := t.lookup("node", e.Node)
	if err != nil {
		return err
	}
	if n.parent == nil {
		return errors.New("the document cannot be removed")
	}
	detach(n)
	n.walk(func(d *node) { delete(t.byID, d.id) })
	return nil
}

func (t *tree) add(e Edit) error {
	parent, err := t.containerFor(e.Parent)
	if err != nil {
		return err
	}
	n, err := nodeFromJSON(e.NodeJSON)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	clash := ""
	n.walk(func(d *node) {
		if (t.byID[d.id] != nil || seen[d.id]) && clash == "" {
			clash = d.id
		}
		seen[d.id] = true
	})
	if clash != "" {
		return fmt.Errorf("node id %q already exists", clash)
	}
	insert(parent, n, e.Index)
	n.walk(func(d *node) { t.byID[d.id] = d })
	return nil
}

func (t *tree) move(e Edit) error {
	n, err := t.lookup("node", e.Node)
	if err != nil {
		return err
	}
	parent, err := t.containerFor(e.Parent)
	if err != nil {
		return err
	}
	if n.parent == nil {
		return errors.New("the document cannot be moved")
	}
	for p := parent; p != nil; p = p.parent {
		if p == n {
			return fmt.Errorf("cannot move %q under itself", n.id)
		}
	}
	detach(n)
	insert(parent, n, e.Index)
	return nil
}

func (t *tree) containerFor(id string) (*node, error) {
	parent, err := t.lookup("parent", id)
	if err != nil {
		return nil, err
	}
	if !parent.container {
		return nil, fmt.Errorf("parent %q is a %s, which cannot have children", id, parent.typ)
	}
	return parent, nil
}

func detach(n *node) {
	n.parent.children = slices.DeleteFunc(n.parent.children, func(k *node) bool { return k == n })
	n.parent = nil
}

func insert(parent, n *node, index int) {
	if index < 0 || index > len(parent.children) {
		index = len(parent.children)
	}
	parent.children = slices.Insert(parent.children, index, n)
	n.parent = parent
}

// canonicalJSON round-trips v through JSON so Go callers' typed values
// ([]map[string]any, structs, ints) take the shapes the fixture's own
// properties use: props, []any, float64.
func canonicalJSON(v any) (any, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("value is not JSON: %w", err)
	}
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("value is not JSON: %w", err)
	}
	return out, nil
}

func nodeFromJSON(raw json.RawMessage) (*node, error) {
	var obj props
	if err := json.Unmarshal(raw, &obj); err != nil || obj == nil {
		return nil, errors.New("node_json must be a JSON object")
	}
	return buildNode(obj)
}

// buildNode turns a decoded Figma node object into a fixture node: id, type,
// name and children become fields, fillGeometry and strokeGeometry the
// geometry=paths output (derived from the bounding box when absent), and
// every other key a property served as given.
func buildNode(obj props) (*node, error) {
	id, _ := obj["id"].(string)
	typ, _ := obj["type"].(string)
	if id == "" || typ == "" {
		return nil, errors.New("node_json needs a string id and type")
	}
	name, _ := obj["name"].(string)
	rawKids, hasKids := obj["children"]
	kidList, ok := rawKids.([]any)
	if hasKids && !ok {
		return nil, fmt.Errorf("node %q: children must be a list", id)
	}
	if len(kidList) > 0 && !containerTypes[typ] {
		return nil, fmt.Errorf("node %q: a %s cannot have children", id, typ)
	}
	kids := make([]*node, 0, len(kidList))
	for _, raw := range kidList {
		kidObj, ok := raw.(props)
		if !ok {
			return nil, fmt.Errorf("node %q: each child must be an object", id)
		}
		kid, err := buildNode(kidObj)
		if err != nil {
			return nil, err
		}
		kids = append(kids, kid)
	}
	p := props{}
	geometry := props{}
	for k, v := range obj {
		switch k {
		case "id", "type", "name", "children":
		case "fillGeometry", "strokeGeometry":
			geometry[k] = v
		default:
			p[k] = v
		}
	}
	n := el(typ, id, name, p, kids...)
	if len(geometry) > 0 {
		n.geometry = geometry
		return n, nil
	}
	addGeometry(n)
	return n, nil
}

// clone deep-copies the tree so an edit never touches a published version.
func (t *tree) clone() *tree {
	return newTree(cloneNode(t.root))
}

func cloneNode(n *node) *node {
	kids := make([]*node, 0, len(n.children))
	for _, k := range n.children {
		kids = append(kids, cloneNode(k))
	}
	out := el(n.typ, n.id, n.name, copyProps(n.props), kids...)
	out.geometry = copyProps(n.geometry)
	out.container = n.container
	return out
}

func copyProps(p props) props {
	if p == nil {
		return nil
	}
	out := make(props, len(p))
	for k, v := range p {
		out[k] = deepCopy(v)
	}
	return out
}

func deepCopy(v any) any {
	switch v := v.(type) {
	case props:
		return copyProps(v)
	case []any:
		out := make([]any, len(v))
		for i, x := range v {
			out[i] = deepCopy(x)
		}
		return out
	}
	return v
}
