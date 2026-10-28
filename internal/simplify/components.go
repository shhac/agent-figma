package simplify

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/shhac/agent-figma/internal/cache"
	"github.com/shhac/agent-figma/internal/figma"
)

const maxVariantNodes = 10

// CodeRef is the code component a design-system component maps to, from
// the repository's mapping.
type CodeRef struct {
	Component string         `json:"component"`
	Import    string         `json:"import,omitempty"`
	Notes     string         `json:"notes,omitempty"`
	Props     map[string]any `json:"props,omitempty"`
}

// ComponentUse is one component set (or standalone component) used in the
// output, with the variants used and where.
type ComponentUse struct {
	Set            string `json:"set,omitempty"`
	Component      string `json:"component,omitempty"`
	ComponentSetID string `json:"component_set_id,omitempty"`
	ComponentID    string `json:"component_id,omitempty"`
	SetKey         string `json:"set_key,omitempty"`
	Key            string `json:"key,omitempty"`
	Remote         bool   `json:"remote,omitempty"`
	Count          int    `json:"count"`
	// Mapped and Code come from the repository's component mapping.
	Mapped       *bool        `json:"mapped,omitempty"`
	Code         *CodeRef     `json:"code,omitempty"`
	Variants     []VariantUse `json:"variants,omitempty"`
	variantIndex map[string]int
}

type VariantUse struct {
	Props     map[string]any `json:"props,omitempty"`
	CodeProps map[string]any `json:"code_props,omitempty"`
	Count     int            `json:"count"`
	Nodes     []string       `json:"nodes"`
}

type componentUses struct {
	store *cache.NodeStore
	byKey map[string]*ComponentUse
}

func newComponentUses(store *cache.NodeStore) *componentUses {
	return &componentUses{store: store, byKey: map[string]*ComponentUse{}}
}

// add counts one instance under its component set, or its component when it
// is not a variant.
func (u *componentUses) add(id string, n figma.Node) {
	c, known := u.store.Components[n.ComponentID]
	use := &ComponentUse{ComponentID: n.ComponentID, Component: c.Name, Key: c.Key, Remote: c.Remote}
	key := "c:" + n.ComponentID
	if set, ok := u.store.ComponentSets[c.ComponentSetID]; known && ok {
		key = "s:" + c.ComponentSetID
		use = &ComponentUse{Set: set.Name, ComponentSetID: c.ComponentSetID, SetKey: set.Key, Remote: set.Remote}
	}
	if existing, ok := u.byKey[key]; ok {
		use = existing
	} else {
		use.variantIndex = map[string]int{}
		u.byKey[key] = use
	}
	use.Count++
	props := variantPropsOfInstance(n)
	vkey := propsKey(props)
	i, ok := use.variantIndex[vkey]
	if !ok {
		i = len(use.Variants)
		use.variantIndex[vkey] = i
		use.Variants = append(use.Variants, VariantUse{Props: props})
	}
	v := &use.Variants[i]
	v.Count++
	if len(v.Nodes) < maxVariantNodes {
		v.Nodes = append(v.Nodes, id)
	}
}

// list orders uses by count, then name, and each one's variants by count.
func (u *componentUses) list() []ComponentUse {
	out := make([]ComponentUse, 0, len(u.byKey))
	for _, use := range u.byKey {
		slices.SortStableFunc(use.Variants, func(a, b VariantUse) int { return cmp.Compare(b.Count, a.Count) })
		out = append(out, *use)
	}
	slices.SortFunc(out, func(a, b ComponentUse) int {
		if c := cmp.Compare(b.Count, a.Count); c != 0 {
			return c
		}
		return cmp.Compare(cmp.Or(a.Set, a.Component), cmp.Or(b.Set, b.Component))
	})
	return out
}

// variantPropsOfInstance keeps only VARIANT properties: they identify which
// variant is used, while text and boolean props are per call site.
func variantPropsOfInstance(n figma.Node) map[string]any {
	out := map[string]any{}
	for name, p := range n.ComponentProperties {
		if p.Type == "VARIANT" {
			out[propName(name)] = p.Value
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func propsKey(props map[string]any) string {
	var b strings.Builder
	for _, k := range slices.Sorted(maps.Keys(props)) {
		b.WriteString(k + "=" + fmt.Sprint(props[k]) + ";")
	}
	return b.String()
}

func (w *walker) componentInfo(rec *Record, n figma.Node, stored *cache.StoredNode) {
	switch n.Type {
	case "INSTANCE":
		if n.ComponentID != "" {
			ref := &ComponentRef{ID: n.ComponentID}
			if c, ok := w.store.Components[n.ComponentID]; ok {
				ref.Name = c.Name
				if set, ok := w.store.ComponentSets[c.ComponentSetID]; ok {
					ref.Set = set.Name
				}
			}
			rec.Component = ref
		}
		rec.Props = instanceProps(n.ComponentProperties)
	case "COMPONENT":
		if parent, ok := w.node(stored.Parent); ok && parent.Type == "COMPONENT_SET" {
			rec.Props = VariantProps(n.Name)
		}
	case "COMPONENT_SET":
		rec.Variants = variantOptions(n.ComponentPropertyDefinitions)
	}
}

// propName drops the "#12:3" suffix Figma appends to non-variant property names.
func propName(name string) string {
	if i := strings.LastIndex(name, "#"); i > 0 {
		return name[:i]
	}
	return name
}

func instanceProps(props map[string]figma.ComponentProperty) map[string]any {
	if len(props) == 0 {
		return nil
	}
	out := make(map[string]any, len(props))
	for name, p := range props {
		out[propName(name)] = p.Value
	}
	return out
}

// VariantProps parses a variant component's name ("State=Hover, Size=md").
func VariantProps(name string) map[string]any {
	out := map[string]any{}
	for _, part := range strings.Split(name, ",") {
		k, v, ok := strings.Cut(part, "=")
		if ok {
			out[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func variantOptions(defs map[string]figma.ComponentPropertyDef) map[string][]string {
	out := map[string][]string{}
	for name, d := range defs {
		if d.Type == "VARIANT" && len(d.VariantOptions) > 0 {
			out[propName(name)] = d.VariantOptions
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
