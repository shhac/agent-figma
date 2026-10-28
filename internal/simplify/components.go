package simplify

import (
	"cmp"
	"slices"
	"strings"

	"github.com/shhac/agent-figma/internal/cache"
	"github.com/shhac/agent-figma/internal/figma"
)

const maxVariantNodes = 10

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
	Mapped       *bool          `json:"mapped,omitempty"`
	Code         map[string]any `json:"code,omitempty"`
	Variants     []VariantUse   `json:"variants,omitempty"`
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
	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k)
		b.WriteString("=")
		if s, ok := props[k].(string); ok {
			b.WriteString(s)
		}
		b.WriteString(";")
	}
	return b.String()
}
