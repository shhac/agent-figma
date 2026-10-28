package mockfigma

import "fmt"

// Synthetic identifiers. Nothing here comes from a real Figma account.
const (
	FileKey    = "FAKEFILEKEY0000000000000"
	BranchKey  = "FAKEBRANCHKEY00000000000"
	FileName   = "Example File"
	BranchName = "Example Branch"

	// InitialVersion is the main file's version after New or Reset.
	InitialVersion       = "1000000000000000001"
	InitialBranchVersion = "1000000000000000101"

	// ExpiredToken is the token the mock rejects as invalid or expired.
	ExpiredToken = "expired-token"

	ImageRefHero  = "fakeimageref0001"
	ImageRefCover = "fakeimageref0002"
)

// Node ids in the fixture document.
const (
	NodeDocument       = "0:0"
	NodeCoverPage      = "0:1"
	NodeCoverFrame     = "0:2"
	NodeCoverTitle     = "0:3"
	NodeComponentsPage = "1:1"
	NodeButtonSet      = "1:10"
	NodeButtonDefault  = "1:11"
	NodeButtonHover    = "1:12"
	NodeButtonDisabled = "1:13"
	NodeIconStar       = "1:20"
	NodeScreensPage    = "2:1"
	NodeCheckout       = "2:10"
	NodeButtonInstance = "2:11"
	NodeTotalText      = "2:12"
	NodeHeroImage      = "2:13"
	NodeFrame12        = "2:14"
	NodeLogoGroup      = "2:15"
	NodeHiddenNote     = "2:16"
	NodeExportFrame    = "2:17"
	NodeHoverInstance  = "2:18"
	NodeBadge          = "2:20"
	NodeCard           = "2:30"
	NodeCardBody       = "2:31"
	NodePriceRow       = "2:32"
	NodePriceText      = "2:33"
	NodeStatusDot      = "2:34"
	// NodeInstanceLabel is a node inside an instance, in Figma's I<instance>;<main> form.
	NodeInstanceLabel = "I2:11;1:14"
)

// Local style ids, as the styles map on nodes refers to them.
const (
	StyleBrandPrimary = "9:1"
	StyleHeadingH1    = "9:2"
	StyleBodyLabel    = "9:3"
	StyleCardShadow   = "9:4"
)

var (
	colorBrand   = rgba(0.231, 0.357, 0.961, 1)
	colorWhite   = rgba(1, 1, 1, 1)
	colorInk     = rgba(0.067, 0.075, 0.094, 1)
	colorCanvas  = rgba(0.961, 0.961, 0.961, 1)
	colorSurface = rgba(0.953, 0.957, 0.965, 1)
	colorBorder  = rgba(0.886, 0.894, 0.91, 1)
	colorAlert   = rgba(0.902, 0.224, 0.275, 1)
	colorOK      = rgba(0.133, 0.773, 0.369, 1)
)

func buildDocument() *node {
	root := el("DOCUMENT", NodeDocument, "Document", props{"scrollBehavior": "SCROLLS"},
		canvas(NodeCoverPage, "Cover", coverFrame()),
		canvas(NodeComponentsPage, "Components", buttonSet(), iconStar()),
		canvas(NodeScreensPage, "Screens", checkout()),
	)
	root.walk(addGeometry)
	return root
}

func canvas(id, name string, kids ...*node) *node {
	return el("CANVAS", id, name, props{
		"backgroundColor":      colorCanvas,
		"prototypeStartNodeID": nil,
		"flowStartingPoints":   []any{},
		"prototypeDevice":      props{"type": "NONE", "rotation": "NONE"},
		"scrollBehavior":       "SCROLLS",
	}, kids...)
}

func coverFrame() *node {
	return el("FRAME", NodeCoverFrame, "Cover", merge(
		box(0, 0, 1440, 960),
		frameStyle(paints(imagePaint(ImageRefCover, "FIT"))),
	),
		text(NodeCoverTitle, "Example File", "Example File", 80, 80, 600, 56, typeH1(), props{
			"styles": props{"text": StyleHeadingH1},
		}),
	)
}

func buttonSet() *node {
	return el("COMPONENT_SET", NodeButtonSet, "Button", merge(
		box(0, 0, 424, 72),
		frameStyle(paints()),
		props{
			"strokes":      paints(solid(rgba(0.592, 0.278, 1, 1))),
			"dashPattern":  []any{10, 5},
			"cornerRadius": 5,
			"componentPropertyDefinitions": props{
				"State":     props{"type": "VARIANT", "defaultValue": "Default", "variantOptions": []any{"Default", "Hover", "Disabled"}},
				"Size":      props{"type": "VARIANT", "defaultValue": "md", "variantOptions": []any{"md"}},
				"Label#1:5": props{"type": "TEXT", "defaultValue": "Button"},
			},
		},
	),
		buttonVariant(NodeButtonDefault, "State=Default, Size=md", "1:14", 16, nil),
		buttonVariant(NodeButtonHover, "State=Hover, Size=md", "1:15", 152, props{
			"effects": []any{dropShadow(0.24, 4, 12)},
		}),
		buttonVariant(NodeButtonDisabled, "State=Disabled, Size=md", "1:16", 288, props{
			"opacity": 0.5,
		}),
	)
}

func buttonVariant(id, name, labelID string, x float64, extra props) *node {
	return el("COMPONENT", id, name, merge(buttonFrame(x, 16), extra),
		buttonLabel(labelID, "Button", x+16, 26),
	)
}

// buttonFrame is the layout and paint a Button variant and its instances share.
func buttonFrame(x, y float64) props {
	return merge(
		box(x, y, 120, 40),
		frameStyle(paints(solid(colorBrand))),
		autoLayout("HORIZONTAL", 8, 16, 10),
		props{
			"primaryAxisAlignItems":  "CENTER",
			"counterAxisAlignItems":  "CENTER",
			"primaryAxisSizingMode":  "AUTO",
			"counterAxisSizingMode":  "AUTO",
			"layoutSizingHorizontal": "HUG",
			"layoutSizingVertical":   "HUG",
			"cornerRadius":           8,
			"styles":                 props{"fill": StyleBrandPrimary},
		},
	)
}

func buttonLabel(id, chars string, x, y float64) *node {
	return text(id, "Label", chars, x, y, 88, 20, typeLabel(), props{
		"fills":                       paints(solid(colorWhite)),
		"styles":                      props{"text": StyleBodyLabel},
		"componentPropertyReferences": props{"characters": "Label#1:5"},
		"layoutSizingHorizontal":      "HUG",
		"layoutSizingVertical":        "HUG",
	})
}

func iconStar() *node {
	return el("COMPONENT", NodeIconStar, "Icon/Star", merge(
		box(0, 120, 24, 24),
		frameStyle(paints()),
	),
		el("VECTOR", "1:21", "Star", merge(box(2, 122, 20, 20), shapeStyle(paints(solid(colorBrand))))),
		el("ELLIPSE", "1:22", "Dot", merge(box(10, 130, 4, 4), shapeStyle(paints(solid(colorWhite))), props{
			"arcData": props{"startingAngle": 0, "endingAngle": 6.2831855, "innerRadius": 0},
		})),
	)
}

func checkout() *node {
	return el("FRAME", NodeCheckout, "Checkout", merge(
		box(0, 0, 375, 940),
		frameStyle(paints(solid(colorWhite))),
		autoLayout("VERTICAL", 16, 24, 24),
		props{
			"primaryAxisAlignItems":  "MIN",
			"counterAxisAlignItems":  "MIN",
			"primaryAxisSizingMode":  "FIXED",
			"counterAxisSizingMode":  "FIXED",
			"layoutSizingHorizontal": "FIXED",
			"layoutSizingVertical":   "FIXED",
		},
	),
		buttonInstance(NodeButtonInstance, NodeButtonDefault, "1:14", "Default", "Pay now", 24),
		text(NodeTotalText, "Total: $42.00", "Total: $42.00", 24, 80, 327, 24, typeBody(), props{
			// Figma lists a style override index per character; "$42.00" is bold.
			"characterStyleOverrides": []any{0, 0, 0, 0, 0, 0, 0, 1, 1, 1, 1, 1, 1},
			"styleOverrideTable": props{
				"1": props{"fontWeight": 700, "fontPostScriptName": "Inter-Bold", "fontStyle": "Bold"},
			},
			"layoutAlign":            "STRETCH",
			"layoutSizingHorizontal": "FILL",
			"layoutSizingVertical":   "HUG",
		}),
		el("RECTANGLE", NodeHeroImage, "Hero image", merge(
			box(24, 120, 327, 180),
			shapeStyle(paints(imagePaint(ImageRefHero, "FILL"))),
			props{"cornerRadius": 12, "layoutSizingHorizontal": "FILL", "layoutSizingVertical": "FIXED"},
		)),
		el("FRAME", NodeFrame12, "Frame 12", merge(
			box(24, 316, 327, 120),
			frameStyle(paints(solid(colorSurface))),
		),
			el("RECTANGLE", NodeBadge, "Badge", merge(
				box(311, 324, 24, 24),
				shapeStyle(paints(solid(colorAlert))),
				props{
					"cornerRadius": 12,
					"constraints":  props{"vertical": "TOP", "horizontal": "RIGHT"},
				},
			)),
		),
		el("GROUP", NodeLogoGroup, "Logo", merge(
			box(24, 452, 48, 48),
			frameStyle(paints()),
			props{"clipsContent": false},
		),
			el("VECTOR", "2:21", "Logo mark", merge(box(24, 452, 48, 32), shapeStyle(paints(solid(colorBrand))))),
			el("VECTOR", "2:22", "Logo type", merge(box(24, 488, 48, 12), shapeStyle(paints(solid(colorInk))))),
		),
		el("RECTANGLE", NodeHiddenNote, "Hidden note", merge(
			box(24, 516, 327, 40),
			shapeStyle(paints(solid(colorSurface))),
			props{"visible": false, "absoluteRenderBounds": nil},
		)),
		el("FRAME", NodeExportFrame, "Export me", merge(
			box(24, 516, 120, 120),
			frameStyle(paints(linearGradient(colorBrand, colorOK))),
			props{
				"cornerRadius": 16,
				"exportSettings": []any{
					props{"suffix": "@2x", "format": "PNG", "constraint": props{"type": "SCALE", "value": 2}},
					props{"suffix": "", "format": "SVG", "constraint": props{"type": "SCALE", "value": 1}},
				},
			},
		),
			el("RECTANGLE", "2:23", "Swatch", merge(box(40, 532, 88, 88), shapeStyle(paints(solid(colorWhite))))),
		),
		buttonInstance(NodeHoverInstance, NodeButtonHover, "1:15", "Hover", "Button", 652),
		card(),
	)
}

func buttonInstance(id, componentID, labelID, state, label string, y float64) *node {
	labelInstanceID := "I" + id + ";" + labelID
	return el("INSTANCE", id, "Button", merge(buttonFrame(24, y), props{
		"componentId": componentID,
		"componentProperties": props{
			"State":     props{"type": "VARIANT", "value": state},
			"Size":      props{"type": "VARIANT", "value": "md"},
			"Label#1:5": props{"type": "TEXT", "value": label},
		},
		"overrides":              []any{props{"id": labelInstanceID, "overriddenFields": []any{"characters"}}},
		"layoutSizingHorizontal": "HUG",
	}),
		buttonLabel(labelInstanceID, label, 40, y+10),
	)
}

// card nests four levels below Checkout, for depth-limit tests.
func card() *node {
	return el("FRAME", NodeCard, "Card", merge(
		box(24, 708, 327, 120),
		frameStyle(paints(solid(colorWhite))),
		autoLayout("HORIZONTAL", 12, 16, 16),
		props{
			"strokes":                paints(solid(colorBorder)),
			"effects":                []any{dropShadow(0.12, 2, 8)},
			"styles":                 props{"effect": StyleCardShadow},
			"cornerRadius":           12,
			"primaryAxisAlignItems":  "MIN",
			"counterAxisAlignItems":  "CENTER",
			"layoutSizingHorizontal": "FILL",
			"layoutSizingVertical":   "HUG",
		},
	),
		el("FRAME", NodeCardBody, "Card body", merge(
			box(40, 724, 295, 88),
			frameStyle(paints()),
			autoLayout("VERTICAL", 8, 0, 0),
			props{"layoutGrow": 1, "layoutSizingHorizontal": "FILL", "layoutSizingVertical": "HUG"},
		),
			el("FRAME", NodePriceRow, "Price row", merge(
				box(40, 724, 295, 24),
				frameStyle(paints()),
				autoLayout("HORIZONTAL", 8, 0, 0),
				props{
					"primaryAxisAlignItems":  "SPACE_BETWEEN",
					"counterAxisAlignItems":  "CENTER",
					"layoutSizingHorizontal": "FILL",
					"layoutSizingVertical":   "HUG",
				},
			),
				text(NodePriceText, "$42.00", "$42.00", 40, 724, 64, 24, typeBody(), nil),
				el("ELLIPSE", NodeStatusDot, "Status dot", merge(
					box(327, 728, 8, 8),
					shapeStyle(paints(solid(colorOK))),
					props{
						"layoutPositioning": "ABSOLUTE",
						"constraints":       props{"vertical": "TOP", "horizontal": "RIGHT"},
						"arcData":           props{"startingAngle": 0, "endingAngle": 6.2831855, "innerRadius": 0},
					},
				)),
			),
		),
	)
}

func text(id, name, chars string, x, y, w, h float64, style props, extra props) *node {
	return el("TEXT", id, name, merge(
		box(x, y, w, h),
		shapeStyle(paints(solid(colorInk))),
		props{
			"strokeAlign":             "OUTSIDE",
			"characters":              chars,
			"style":                   style,
			"characterStyleOverrides": []any{},
			"styleOverrideTable":      props{},
			"lineTypes":               []any{"NONE"},
			"lineIndentations":        []any{0},
		},
		extra,
	))
}

func typeLabel() props {
	return props{
		"fontFamily": "Inter", "fontPostScriptName": "Inter-SemiBold", "fontStyle": "Semi Bold",
		"fontWeight": 600, "fontSize": 14, "textAlignHorizontal": "CENTER", "textAlignVertical": "CENTER",
		"letterSpacing": 0, "lineHeightPx": 20, "lineHeightPercent": 100,
		"lineHeightPercentFontSize": 142.85715, "lineHeightUnit": "PIXELS", "textAutoResize": "WIDTH_AND_HEIGHT",
	}
}

func typeBody() props {
	return props{
		"fontFamily": "Inter", "fontPostScriptName": "Inter-Regular", "fontStyle": "Regular",
		"fontWeight": 400, "fontSize": 16, "textAlignHorizontal": "LEFT", "textAlignVertical": "TOP",
		"letterSpacing": 0, "lineHeightPx": 24, "lineHeightPercent": 100,
		"lineHeightPercentFontSize": 150, "lineHeightUnit": "PIXELS", "textAutoResize": "HEIGHT",
	}
}

func typeH1() props {
	return props{
		"fontFamily": "Inter", "fontPostScriptName": "Inter-Bold", "fontStyle": "Bold",
		"fontWeight": 700, "fontSize": 48, "textAlignHorizontal": "LEFT", "textAlignVertical": "TOP",
		"letterSpacing": -0.96, "lineHeightPx": 56, "lineHeightPercent": 100,
		"lineHeightPercentFontSize": 116.66667, "lineHeightUnit": "PIXELS", "textAutoResize": "HEIGHT",
	}
}

func merge(parts ...props) props {
	out := props{}
	for _, p := range parts {
		for k, v := range p {
			out[k] = v
		}
	}
	return out
}

func box(x, y, w, h float64) props {
	return props{
		"absoluteBoundingBox":  rect(x, y, w, h),
		"absoluteRenderBounds": rect(x, y, w, h),
		"constraints":          props{"vertical": "TOP", "horizontal": "LEFT"},
	}
}

func rect(x, y, w, h float64) props {
	return props{"x": x, "y": y, "width": w, "height": h}
}

func rgba(r, g, b, a float64) props {
	return props{"r": r, "g": g, "b": b, "a": a}
}

func paints(p ...props) []any {
	out := make([]any, 0, len(p))
	for _, paint := range p {
		out = append(out, paint)
	}
	return out
}

func solid(color props) props {
	return props{"blendMode": "NORMAL", "type": "SOLID", "color": color}
}

func imagePaint(ref, scaleMode string) props {
	return props{"blendMode": "NORMAL", "type": "IMAGE", "scaleMode": scaleMode, "imageRef": ref}
}

func linearGradient(from, to props) props {
	return props{
		"blendMode": "NORMAL",
		"type":      "GRADIENT_LINEAR",
		"gradientHandlePositions": []any{
			props{"x": 0, "y": 0}, props{"x": 1, "y": 1}, props{"x": -0.5, "y": 0.5},
		},
		"gradientStops": []any{
			props{"color": from, "position": 0},
			props{"color": to, "position": 1},
		},
	}
}

func dropShadow(alpha, offsetY, radius float64) props {
	return props{
		"type": "DROP_SHADOW", "visible": true, "blendMode": "NORMAL",
		"color":  rgba(0, 0, 0, alpha),
		"offset": props{"x": 0, "y": offsetY},
		"radius": radius, "spread": 0, "showShadowBehindNode": false,
	}
}

func frameStyle(fills []any) props {
	return props{
		"blendMode":    "PASS_THROUGH",
		"clipsContent": true,
		"fills":        fills,
		"strokes":      []any{},
		"strokeWeight": 1,
		"strokeAlign":  "INSIDE",
		"effects":      []any{},
	}
}

func shapeStyle(fills []any) props {
	return props{
		"blendMode":    "NORMAL",
		"fills":        fills,
		"strokes":      []any{},
		"strokeWeight": 1,
		"strokeAlign":  "INSIDE",
		"effects":      []any{},
	}
}

func autoLayout(mode string, spacing, padX, padY float64) props {
	return props{
		"layoutMode":    mode,
		"itemSpacing":   spacing,
		"paddingLeft":   padX,
		"paddingRight":  padX,
		"paddingTop":    padY,
		"paddingBottom": padY,
		"layoutWrap":    "NO_WRAP",
	}
}

var vectorTypes = map[string]bool{"VECTOR": true, "ELLIPSE": true, "RECTANGLE": true, "STAR": true, "LINE": true, "REGULAR_POLYGON": true}

func addGeometry(n *node) {
	if !vectorTypes[n.typ] {
		return
	}
	bounds, _ := n.props["absoluteBoundingBox"].(props)
	w, h := bounds["width"], bounds["height"]
	n.geometry = props{
		"fillGeometry": []any{props{
			"path":        fmt.Sprintf("M0 0L%v 0L%v %vL0 %vL0 0Z", w, w, h, h),
			"windingRule": "NONZERO",
		}},
		"strokeGeometry": []any{},
	}
}
