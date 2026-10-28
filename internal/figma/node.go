package figma

// Node is one node's own properties, without its children.
type Node struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Type    string   `json:"type"`
	Visible *bool    `json:"visible,omitempty"`
	Opacity *float64 `json:"opacity,omitempty"`

	AbsoluteBoundingBox *Rect `json:"absoluteBoundingBox,omitempty"`
	// AbsoluteRenderBounds includes effects such as shadows, which renders do.
	AbsoluteRenderBounds *Rect `json:"absoluteRenderBounds,omitempty"`

	LayoutMode             string   `json:"layoutMode,omitempty"`
	LayoutWrap             string   `json:"layoutWrap,omitempty"`
	PrimaryAxisAlignItems  string   `json:"primaryAxisAlignItems,omitempty"`
	CounterAxisAlignItems  string   `json:"counterAxisAlignItems,omitempty"`
	PaddingLeft            float64  `json:"paddingLeft,omitempty"`
	PaddingRight           float64  `json:"paddingRight,omitempty"`
	PaddingTop             float64  `json:"paddingTop,omitempty"`
	PaddingBottom          float64  `json:"paddingBottom,omitempty"`
	ItemSpacing            float64  `json:"itemSpacing,omitempty"`
	CounterAxisSpacing     *float64 `json:"counterAxisSpacing,omitempty"`
	LayoutSizingHorizontal string   `json:"layoutSizingHorizontal,omitempty"`
	LayoutSizingVertical   string   `json:"layoutSizingVertical,omitempty"`
	LayoutPositioning      string   `json:"layoutPositioning,omitempty"`
	ClipsContent           bool     `json:"clipsContent,omitempty"`
	OverflowDirection      string   `json:"overflowDirection,omitempty"`

	Fills                   []Paint           `json:"fills,omitempty"`
	Strokes                 []Paint           `json:"strokes,omitempty"`
	StrokeWeight            *float64          `json:"strokeWeight,omitempty"`
	StrokeAlign             string            `json:"strokeAlign,omitempty"`
	StrokeDashes            []float64         `json:"strokeDashes,omitempty"`
	IndividualStrokeWeights *StrokeWeights    `json:"individualStrokeWeights,omitempty"`
	CornerRadius            *float64          `json:"cornerRadius,omitempty"`
	RectangleCornerRadii    []float64         `json:"rectangleCornerRadii,omitempty"`
	Effects                 []Effect          `json:"effects,omitempty"`
	Styles                  map[string]string `json:"styles,omitempty"`

	Characters              string               `json:"characters,omitempty"`
	Style                   *TypeStyle           `json:"style,omitempty"`
	CharacterStyleOverrides []int                `json:"characterStyleOverrides,omitempty"`
	StyleOverrideTable      map[string]TypeStyle `json:"styleOverrideTable,omitempty"`

	ComponentID                  string                          `json:"componentId,omitempty"`
	ComponentProperties          map[string]ComponentProperty    `json:"componentProperties,omitempty"`
	ComponentPropertyDefinitions map[string]ComponentPropertyDef `json:"componentPropertyDefinitions,omitempty"`
	ExportSettings               []ExportSetting                 `json:"exportSettings,omitempty"`
	// Annotations are Dev Mode annotations, when the file has any.
	Annotations []Annotation `json:"annotations,omitempty"`
}

// IsVisible reports the node's own visibility; Figma omits the field when true.
func (n Node) IsVisible() bool { return n.Visible == nil || *n.Visible }

type Rect struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

type Color struct {
	R float64 `json:"r"`
	G float64 `json:"g"`
	B float64 `json:"b"`
	A float64 `json:"a"`
}

type Vector struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type Paint struct {
	Type                    string         `json:"type"`
	Visible                 *bool          `json:"visible,omitempty"`
	Opacity                 *float64       `json:"opacity,omitempty"`
	Color                   *Color         `json:"color,omitempty"`
	GradientHandlePositions []Vector       `json:"gradientHandlePositions,omitempty"`
	GradientStops           []GradientStop `json:"gradientStops,omitempty"`
	ScaleMode               string         `json:"scaleMode,omitempty"`
	ImageRef                string         `json:"imageRef,omitempty"`
	GifRef                  string         `json:"gifRef,omitempty"`
}

func (p Paint) IsVisible() bool { return p.Visible == nil || *p.Visible }

type GradientStop struct {
	Position float64 `json:"position"`
	Color    Color   `json:"color"`
}

type Effect struct {
	Type    string  `json:"type"`
	Visible *bool   `json:"visible,omitempty"`
	Radius  float64 `json:"radius"`
	Color   *Color  `json:"color,omitempty"`
	Offset  *Vector `json:"offset,omitempty"`
	Spread  float64 `json:"spread,omitempty"`
}

func (e Effect) IsVisible() bool { return e.Visible == nil || *e.Visible }

type StrokeWeights struct {
	Top    float64 `json:"top"`
	Right  float64 `json:"right"`
	Bottom float64 `json:"bottom"`
	Left   float64 `json:"left"`
}

type TypeStyle struct {
	FontFamily                string  `json:"fontFamily,omitempty"`
	FontStyle                 string  `json:"fontStyle,omitempty"`
	FontWeight                float64 `json:"fontWeight,omitempty"`
	FontSize                  float64 `json:"fontSize,omitempty"`
	Italic                    bool    `json:"italic,omitempty"`
	LineHeightPx              float64 `json:"lineHeightPx,omitempty"`
	LineHeightPercentFontSize float64 `json:"lineHeightPercentFontSize,omitempty"`
	LineHeightUnit            string  `json:"lineHeightUnit,omitempty"`
	LetterSpacing             float64 `json:"letterSpacing,omitempty"`
	TextAlignHorizontal       string  `json:"textAlignHorizontal,omitempty"`
	TextAlignVertical         string  `json:"textAlignVertical,omitempty"`
	TextCase                  string  `json:"textCase,omitempty"`
	TextDecoration            string  `json:"textDecoration,omitempty"`
	Hyperlink                 *struct {
		Type   string `json:"type"`
		URL    string `json:"url,omitempty"`
		NodeID string `json:"nodeID,omitempty"`
	} `json:"hyperlink,omitempty"`
}

type ComponentProperty struct {
	Type  string `json:"type"`
	Value any    `json:"value"`
}

type ComponentPropertyDef struct {
	Type           string   `json:"type"`
	DefaultValue   any      `json:"defaultValue"`
	VariantOptions []string `json:"variantOptions,omitempty"`
}

type ExportSetting struct {
	Suffix     string     `json:"suffix"`
	Format     string     `json:"format"`
	Constraint Constraint `json:"constraint"`
}

type Constraint struct {
	Type  string  `json:"type"`
	Value float64 `json:"value"`
}

type Annotation struct {
	Label         string `json:"label,omitempty"`
	LabelMarkdown string `json:"labelMarkdown,omitempty"`
}
