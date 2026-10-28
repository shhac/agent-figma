package mockfigma

// Published library keys. Real keys are 40 hex characters; these are
// deliberately readable fakes.
const (
	ButtonSetKey      = "fakecomponentsetkey0000000000000000000110"
	ButtonDefaultKey  = "fakecomponentkey0000000000000000000000111"
	ButtonHoverKey    = "fakecomponentkey0000000000000000000000112"
	ButtonDisabledKey = "fakecomponentkey0000000000000000000000113"
	IconStarKey       = "fakecomponentkey0000000000000000000000120"
	BrandPrimaryKey   = "fakestylekey0000000000000000000000000091"
	HeadingH1Key      = "fakestylekey0000000000000000000000000092"
	BodyLabelKey      = "fakestylekey0000000000000000000000000093"
	CardShadowKey     = "fakestylekey0000000000000000000000000094"
)

const (
	publishedCreatedAt = "2025-11-03T09:00:00Z"
	publishedUpdatedAt = "2026-01-12T12:00:00Z"
)

type componentInfo struct {
	nodeID, key, name, description, setID string
}

type styleInfo struct {
	nodeID, key, name, description, styleType, sortPosition string
}

var fixtureComponents = []componentInfo{
	{NodeButtonDefault, ButtonDefaultKey, "State=Default, Size=md", "", NodeButtonSet},
	{NodeButtonHover, ButtonHoverKey, "State=Hover, Size=md", "", NodeButtonSet},
	{NodeButtonDisabled, ButtonDisabledKey, "State=Disabled, Size=md", "", NodeButtonSet},
	{NodeIconStar, IconStarKey, "Icon/Star", "Five-point star icon.", ""},
}

var fixtureComponentSets = []componentInfo{
	{NodeButtonSet, ButtonSetKey, "Button", "Primary action button.", ""},
}

var fixtureStyles = []styleInfo{
	{StyleBrandPrimary, BrandPrimaryKey, "Brand/Primary", "Primary brand colour.", "FILL", "a0"},
	{StyleHeadingH1, HeadingH1Key, "Heading/H1", "Page titles.", "TEXT", "a1"},
	{StyleBodyLabel, BodyLabelKey, "Body/Label", "Button and control labels.", "TEXT", "a2"},
	{StyleCardShadow, CardShadowKey, "Elevation/Card", "Resting card shadow.", "EFFECT", "a3"},
}

func designer(base string) props {
	return props{"id": "100000000000000001", "handle": "Example Designer", "img_url": base + "/_cdn/avatar/100000000000000001.png"}
}

func viewer(base string) props {
	return props{"id": "100000000000000002", "handle": "Example User", "img_url": base + "/_cdn/avatar/100000000000000002.png"}
}

func findComponent(list []componentInfo, match func(componentInfo) bool) (componentInfo, bool) {
	for _, c := range list {
		if match(c) {
			return c, true
		}
	}
	return componentInfo{}, false
}

func findStyle(match func(styleInfo) bool) (styleInfo, bool) {
	for _, st := range fixtureStyles {
		if match(st) {
			return st, true
		}
	}
	return styleInfo{}, false
}

// dependencies builds the components, componentSets and styles maps of a file
// or nodes response from the nodes it carries: main components present or
// instanced, their sets, and every style a node binds.
func dependencies(nodes []*node) (components, componentSets, styles props) {
	components, componentSets, styles = props{}, props{}, props{}
	addComponent := func(id string) {
		c, ok := findComponent(fixtureComponents, func(c componentInfo) bool { return c.nodeID == id })
		if !ok {
			return
		}
		entry := props{"key": c.key, "name": c.name, "description": c.description, "remote": false, "documentationLinks": []any{}}
		if c.setID != "" {
			entry["componentSetId"] = c.setID
			addSet(componentSets, c.setID)
		}
		components[id] = entry
	}
	for _, n := range nodes {
		switch n.typ {
		case "COMPONENT":
			addComponent(n.id)
		case "INSTANCE":
			id, _ := n.props["componentId"].(string)
			addComponent(id)
		case "COMPONENT_SET":
			addSet(componentSets, n.id)
		}
		bound, _ := n.props["styles"].(props)
		for _, styleID := range bound {
			id, _ := styleID.(string)
			if st, ok := findStyle(func(st styleInfo) bool { return st.nodeID == id }); ok {
				styles[id] = props{"key": st.key, "name": st.name, "description": st.description, "remote": false, "styleType": st.styleType}
			}
		}
	}
	return components, componentSets, styles
}

func addSet(sets props, id string) {
	set, ok := findComponent(fixtureComponentSets, func(c componentInfo) bool { return c.nodeID == id })
	if !ok {
		return
	}
	sets[id] = props{"key": set.key, "name": set.name, "description": set.description, "remote": false, "documentationLinks": []any{}}
}

// containingFrame is Figma's FrameInfo: the page, plus the top-level frame
// and component set when the node sits inside them. A node that is itself
// top-level on its page has no nodeId/name.
func containingFrame(t *tree, nodeID string) props {
	n := t.byID[nodeID]
	page := n.page()
	out := props{"pageId": page.id, "pageName": page.name}
	top := n
	for top.parent != nil && top.parent != page {
		top = top.parent
	}
	if top != n {
		out["nodeId"] = top.id
		out["name"] = top.name
	}
	if n.parent != nil && n.parent.typ == "COMPONENT_SET" {
		set := props{"nodeId": n.parent.id, "name": n.parent.name}
		out["containingComponentSet"] = set
		out["containingStateGroup"] = set
	}
	return out
}

func publishedComponent(t *tree, c componentInfo, base string) props {
	return props{
		"key":              c.key,
		"file_key":         FileKey,
		"node_id":          c.nodeID,
		"thumbnail_url":    base + "/_cdn/component/" + c.key + ".png",
		"name":             c.name,
		"description":      c.description,
		"created_at":       publishedCreatedAt,
		"updated_at":       publishedUpdatedAt,
		"user":             designer(base),
		"containing_frame": containingFrame(t, c.nodeID),
	}
}

func publishedStyle(st styleInfo, base string) props {
	return props{
		"key":           st.key,
		"file_key":      FileKey,
		"node_id":       st.nodeID,
		"style_type":    st.styleType,
		"thumbnail_url": base + "/_cdn/style/" + st.key + ".png",
		"name":          st.name,
		"description":   st.description,
		"created_at":    publishedCreatedAt,
		"updated_at":    publishedUpdatedAt,
		"user":          designer(base),
		"sort_position": st.sortPosition,
	}
}

const (
	variableCollectionID = "VariableCollectionId:8:0"
	variableBrandID      = "VariableID:8:1"
	variableSpacingID    = "VariableID:8:2"
	variableActionID     = "VariableID:8:3"
)

func localVariables() props {
	alias := props{"type": "VARIABLE_ALIAS", "id": variableBrandID}
	return props{
		"variables": props{
			variableBrandID: localVariable(variableBrandID, "color/brand", "fakevariablekey0001", "COLOR", "Brand colour.",
				props{"8:0": colorBrand, "8:1": rgba(0.459, 0.561, 1, 1)}, []any{"ALL_FILLS"}, props{"WEB": "var(--color-brand)"}),
			variableSpacingID: localVariable(variableSpacingID, "spacing/md", "fakevariablekey0002", "FLOAT", "",
				props{"8:0": 16, "8:1": 16}, []any{"GAP"}, props{}),
			variableActionID: localVariable(variableActionID, "color/action", "fakevariablekey0003", "COLOR", "Alias of color/brand.",
				props{"8:0": alias, "8:1": alias}, []any{"ALL_FILLS"}, props{}),
		},
		"variableCollections": props{
			variableCollectionID: props{
				"id":                   variableCollectionID,
				"name":                 "Tokens",
				"key":                  "fakevariablecollectionkey0001",
				"modes":                []any{props{"modeId": "8:0", "name": "Light"}, props{"modeId": "8:1", "name": "Dark"}},
				"defaultModeId":        "8:0",
				"remote":               false,
				"hiddenFromPublishing": false,
				"variableIds":          []any{variableBrandID, variableSpacingID, variableActionID},
			},
		},
	}
}

func localVariable(id, name, key, resolvedType, description string, values props, scopes []any, codeSyntax props) props {
	return props{
		"id":                   id,
		"name":                 name,
		"key":                  key,
		"variableCollectionId": variableCollectionID,
		"resolvedType":         resolvedType,
		"valuesByMode":         values,
		"remote":               false,
		"description":          description,
		"hiddenFromPublishing": false,
		"scopes":               scopes,
		"codeSyntax":           codeSyntax,
	}
}

func publishedVariables() props {
	published := func(id, name, key, resolved, subscribed string) props {
		return props{
			"id": id, "subscribed_id": subscribed, "name": name, "key": key,
			"variableCollectionId": variableCollectionID, "resolvedDataType": resolved, "updatedAt": publishedUpdatedAt,
		}
	}
	return props{
		"variables": props{
			variableBrandID:   published(variableBrandID, "color/brand", "fakevariablekey0001", "COLOR", "fakesubscribedid0001"),
			variableSpacingID: published(variableSpacingID, "spacing/md", "fakevariablekey0002", "FLOAT", "fakesubscribedid0002"),
		},
		"variableCollections": props{
			variableCollectionID: props{
				"id": variableCollectionID, "subscribed_id": "fakesubscribedid0000", "name": "Tokens",
				"key": "fakevariablecollectionkey0001", "updatedAt": publishedUpdatedAt,
			},
		},
	}
}

func fixtureComments(fileKey, base string) []any {
	pinned := props{"node_id": NodePriceRow, "node_offset": props{"x": 12, "y": 8}}
	return []any{
		props{
			"id": "100001", "file_key": fileKey, "parent_id": "", "user": designer(base),
			"created_at": "2026-01-13T09:15:00Z", "resolved_at": nil,
			"message":     "Should the price row wrap on narrow screens?",
			"client_meta": pinned, "order_id": "1",
			"reactions": []any{props{"user": viewer(base), "emoji": ":+1:", "created_at": "2026-01-13T09:20:00Z"}},
		},
		props{
			"id": "100002", "file_key": fileKey, "parent_id": "100001", "user": viewer(base),
			"created_at": "2026-01-13T10:02:00Z", "resolved_at": nil,
			"message":     "Yes, let it wrap below 320px.",
			"client_meta": pinned, "order_id": nil, "reactions": []any{},
		},
		props{
			"id": "100003", "file_key": fileKey, "parent_id": "", "user": viewer(base),
			"created_at": "2026-01-14T11:00:00Z", "resolved_at": "2026-01-14T15:30:00Z",
			"message":     "Cover copy is final.",
			"client_meta": props{"x": 120, "y": 80}, "order_id": "2", "reactions": []any{},
		},
	}
}

func fixtureDevResources() []props {
	return []props{
		{"id": "fakedevresource0001", "name": "Button in Storybook", "url": "https://example.com/storybook/button", "file_key": FileKey, "node_id": NodeButtonSet},
		{"id": "fakedevresource0002", "name": "Checkout spec", "url": "https://example.com/docs/checkout", "file_key": FileKey, "node_id": NodeCheckout},
	}
}
