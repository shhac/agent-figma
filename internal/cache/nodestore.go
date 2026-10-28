package cache

import (
	"encoding/json"
	"fmt"

	"github.com/shhac/agent-figma/internal/figma"
)

// Full is the KnownDepth of a node whose entire subtree is stored.
const Full = -1

// StoredNode is one node's own properties plus where it sits.
type StoredNode struct {
	Parent string `json:"parent,omitempty"`
	Page   string `json:"page,omitempty"`
	// KnownDepth is how many levels below this node have complete children
	// lists: Full for the whole subtree, 0 for the node's own properties only
	// (its children list is unknown or partial, as with the ancestors Figma
	// returns alongside a requested node).
	KnownDepth int `json:"known_depth"`
	// Children is set only when KnownDepth != 0.
	Children []string        `json:"children,omitempty"`
	Props    json.RawMessage `json:"props"`
}

// NodeStore is everything Figma has returned for one file version.
type NodeStore struct {
	Version       string                        `json:"version"`
	Name          string                        `json:"name,omitempty"`
	LastModified  string                        `json:"last_modified,omitempty"`
	EditorType    string                        `json:"editor_type,omitempty"`
	Root          string                        `json:"root,omitempty"`
	Nodes         map[string]*StoredNode        `json:"nodes"`
	Components    map[string]figma.Component    `json:"components,omitempty"`
	ComponentSets map[string]figma.ComponentSet `json:"component_sets,omitempty"`
	Styles        map[string]figma.Style        `json:"styles,omitempty"`
}

func NewNodeStore(version string) *NodeStore {
	return &NodeStore{Version: version, Nodes: map[string]*StoredNode{}}
}

// Has reports whether id's subtree is stored to at least depth levels below
// it (Full for the whole subtree).
func (s *NodeStore) Has(id string, depth int) bool {
	n := s.Nodes[id]
	if n == nil {
		return false
	}
	if n.KnownDepth == Full {
		return true
	}
	return depth != Full && n.KnownDepth >= depth
}

// Node decodes a stored node's properties.
func (s *NodeStore) Node(id string) (figma.Node, bool) {
	n := s.Nodes[id]
	if n == nil {
		return figma.Node{}, false
	}
	var node figma.Node
	if err := json.Unmarshal(n.Props, &node); err != nil {
		return figma.Node{}, false
	}
	return node, true
}

// Ancestors lists id's known ancestors, nearest first.
func (s *NodeStore) Ancestors(id string) []string {
	var out []string
	seen := map[string]bool{id: true}
	for n := s.Nodes[id]; n != nil && n.Parent != "" && !seen[n.Parent]; n = s.Nodes[n.Parent] {
		out = append(out, n.Parent)
		seen[n.Parent] = true
	}
	return out
}

// mergeMaps folds a response's component, component set, and style maps in.
func (s *NodeStore) mergeMaps(components map[string]figma.Component, sets map[string]figma.ComponentSet, styles map[string]figma.Style) {
	if len(components) > 0 && s.Components == nil {
		s.Components = map[string]figma.Component{}
	}
	for k, v := range components {
		s.Components[k] = v
	}
	if len(sets) > 0 && s.ComponentSets == nil {
		s.ComponentSets = map[string]figma.ComponentSet{}
	}
	for k, v := range sets {
		s.ComponentSets[k] = v
	}
	if len(styles) > 0 && s.Styles == nil {
		s.Styles = map[string]figma.Style{}
	}
	for k, v := range styles {
		s.Styles[k] = v
	}
}

// rawNode is a node as it arrives, children still attached.
type rawNode struct {
	ID       string            `json:"id"`
	Type     string            `json:"type"`
	Children []json.RawMessage `json:"children"`
}

// MergeFile folds in a GET /v1/files/:key response. requested are the ids
// passed as ?ids (empty for a whole-document fetch); depth is the ?depth
// passed (Full when none), counted from the document root.
func (s *NodeStore) MergeFile(resp *figma.FileResponse, requested []string, depth int) error {
	s.Name, s.LastModified, s.EditorType = resp.Name, resp.LastModified, resp.EditorType
	s.mergeMaps(resp.Components, resp.ComponentSets, resp.Styles)
	want := map[string]bool{}
	for _, id := range requested {
		want[id] = true
	}
	known := func(level int, insideRequested bool) int {
		switch {
		case len(requested) > 0 && insideRequested:
			return Full
		case len(requested) > 0:
			// Ancestors and pages come with partial children lists.
			return 0
		case depth == Full:
			return Full
		default:
			return max(depth-level, 0)
		}
	}
	var walk func(raw json.RawMessage, parent, page string, level int, inside bool) (string, error)
	walk = func(raw json.RawMessage, parent, page string, level int, inside bool) (string, error) {
		var rn rawNode
		if err := json.Unmarshal(raw, &rn); err != nil {
			return "", err
		}
		inside = inside || want[rn.ID]
		if rn.Type == "CANVAS" {
			page = rn.ID
		}
		childIDs := make([]string, 0, len(rn.Children))
		for _, child := range rn.Children {
			id, err := walk(child, rn.ID, page, level+1, inside)
			if err != nil {
				return "", err
			}
			childIDs = append(childIDs, id)
		}
		props, err := stripChildren(raw)
		if err != nil {
			return "", err
		}
		s.put(rn.ID, &StoredNode{Parent: parent, Page: page, KnownDepth: known(level, inside), Children: childIDs, Props: props})
		return rn.ID, nil
	}
	root, err := walk(resp.Document, "", "", 0, false)
	if err != nil {
		return fmt.Errorf("decode file document: %w", err)
	}
	s.Root = root
	return nil
}

// MergeNodes folds in a GET /v1/files/:key/nodes response, fetched with
// depth (Full when none) relative to each requested node. It returns the
// requested ids Figma answered with null.
func (s *NodeStore) MergeNodes(resp *figma.NodesResponse, depth int) ([]string, error) {
	s.Name, s.LastModified, s.EditorType = resp.Name, resp.LastModified, resp.EditorType
	var missing []string
	for id, entry := range resp.Nodes {
		if entry == nil || len(entry.Document) == 0 || string(entry.Document) == "null" {
			missing = append(missing, id)
			continue
		}
		s.mergeMaps(entry.Components, entry.ComponentSets, entry.Styles)
		var walk func(raw json.RawMessage, parent, page string, level int) (string, error)
		walk = func(raw json.RawMessage, parent, page string, level int) (string, error) {
			var rn rawNode
			if err := json.Unmarshal(raw, &rn); err != nil {
				return "", err
			}
			if rn.Type == "CANVAS" {
				page = rn.ID
			}
			childIDs := make([]string, 0, len(rn.Children))
			for _, child := range rn.Children {
				cid, err := walk(child, rn.ID, page, level+1)
				if err != nil {
					return "", err
				}
				childIDs = append(childIDs, cid)
			}
			props, err := stripChildren(raw)
			if err != nil {
				return "", err
			}
			kd := Full
			if depth != Full {
				kd = max(depth-level, 0)
			}
			s.put(rn.ID, &StoredNode{Parent: parent, Page: page, KnownDepth: kd, Children: childIDs, Props: props})
			return rn.ID, nil
		}
		if _, err := walk(entry.Document, "", "", 0); err != nil {
			return nil, fmt.Errorf("decode node %s: %w", id, err)
		}
	}
	s.fillPages()
	return missing, nil
}

// put merges one node: the deeper knowledge of its children wins, and known
// placement (parent, page) is never forgotten.
func (s *NodeStore) put(id string, in *StoredNode) {
	if in.KnownDepth == 0 {
		in.Children = nil
	}
	old := s.Nodes[id]
	if old == nil {
		s.Nodes[id] = in
		return
	}
	if in.Parent == "" {
		in.Parent = old.Parent
	}
	if in.Page == "" {
		in.Page = old.Page
	}
	if deeper(old.KnownDepth, in.KnownDepth) {
		in.KnownDepth, in.Children = old.KnownDepth, old.Children
	}
	s.Nodes[id] = in
}

// fillPages propagates page ids down from nodes whose page is known, for
// subtrees that arrived through /nodes without their ancestry.
func (s *NodeStore) fillPages() {
	var down func(id, page string)
	down = func(id, page string) {
		n := s.Nodes[id]
		if n == nil {
			return
		}
		if n.Page == "" {
			n.Page = page
		}
		for _, c := range n.Children {
			if child := s.Nodes[c]; child != nil && child.Page == "" {
				down(c, n.Page)
			}
		}
	}
	for _, n := range s.Nodes {
		if n.Page == "" {
			continue
		}
		for _, c := range n.Children {
			if child := s.Nodes[c]; child != nil && child.Page == "" {
				down(c, n.Page)
			}
		}
	}
}

func deeper(a, b int) bool {
	if a == Full {
		return b != Full
	}
	return b != Full && a > b
}

func stripChildren(raw json.RawMessage) (json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	delete(fields, "children")
	return json.Marshal(fields)
}
