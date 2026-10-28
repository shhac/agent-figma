package simplify

import (
	"fmt"
	"math"
	"strings"

	"github.com/shhac/agent-figma/internal/figma"
)

func round(v float64) float64 { return math.Round(v*100) / 100 }

func fmtNum(v float64) string { return fmt.Sprintf("%gpx", round(v)) }

func isAutoLayout(n figma.Node) bool {
	return n.LayoutMode == "HORIZONTAL" || n.LayoutMode == "VERTICAL" || n.LayoutMode == "GRID"
}

// position is a node's offset within its parent, emitted only when the
// parent's auto layout does not place it.
func position(n, parent figma.Node) []float64 {
	if n.AbsoluteBoundingBox == nil || parent.AbsoluteBoundingBox == nil {
		return nil
	}
	if isAutoLayout(parent) && n.LayoutPositioning != "ABSOLUTE" {
		return nil
	}
	if parent.Type == "CANVAS" || parent.Type == "DOCUMENT" {
		return nil
	}
	return []float64{
		round(n.AbsoluteBoundingBox.X - parent.AbsoluteBoundingBox.X),
		round(n.AbsoluteBoundingBox.Y - parent.AbsoluteBoundingBox.Y),
	}
}

var axisAlign = map[string]string{"MIN": "start", "CENTER": "center", "MAX": "end", "SPACE_BETWEEN": "space-between", "BASELINE": "baseline"}

var sizing = map[string]string{"FIXED": "fixed", "HUG": "hug", "FILL": "fill"}

// layoutOf describes a node's own auto layout and how its parent sizes it.
func layoutOf(n, parent figma.Node, hasParent bool) map[string]any {
	out := map[string]any{}
	if isAutoLayout(n) {
		out["mode"] = map[string]string{"HORIZONTAL": "row", "VERTICAL": "column", "GRID": "grid"}[n.LayoutMode]
		if n.ItemSpacing != 0 {
			out["gap"] = fmtNum(n.ItemSpacing)
		}
		if n.CounterAxisSpacing != nil && *n.CounterAxisSpacing != 0 {
			out["cross_gap"] = fmtNum(*n.CounterAxisSpacing)
		}
		if p := padding(n); p != "" {
			out["padding"] = p
		}
		if v, ok := axisAlign[n.PrimaryAxisAlignItems]; ok && v != "start" {
			out["justify"] = v
		}
		if v, ok := axisAlign[n.CounterAxisAlignItems]; ok && v != "start" {
			out["align"] = v
		}
		if n.LayoutWrap == "WRAP" {
			out["wrap"] = true
		}
	}
	if hasParent && isAutoLayout(parent) {
		if n.LayoutPositioning == "ABSOLUTE" {
			out["position"] = "absolute"
		}
		h, v := sizing[n.LayoutSizingHorizontal], sizing[n.LayoutSizingVertical]
		if h != "" || v != "" {
			out["sizing"] = map[string]string{"h": h, "v": v}
		}
	}
	if n.ClipsContent && n.OverflowDirection != "" && n.OverflowDirection != "NONE" {
		out["scroll"] = strings.ToLower(strings.TrimSuffix(n.OverflowDirection, "_SCROLLING"))
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// padding is CSS shorthand ("8px 16px").
func padding(n figma.Node) string {
	t, r, b, l := n.PaddingTop, n.PaddingRight, n.PaddingBottom, n.PaddingLeft
	switch {
	case t == 0 && r == 0 && b == 0 && l == 0:
		return ""
	case t == r && r == b && b == l:
		return fmtNum(t)
	case t == b && r == l:
		return fmtNum(t) + " " + fmtNum(r)
	default:
		return strings.Join([]string{fmtNum(t), fmtNum(r), fmtNum(b), fmtNum(l)}, " ")
	}
}

func hasImageFill(paints []figma.Paint) bool {
	for _, p := range paints {
		if p.IsVisible() && p.Type == "IMAGE" {
			return true
		}
	}
	return false
}

// paints renders visible paints top-first, CSS style; one paint is returned
// on its own rather than as a list.
func paints(list []figma.Paint) any {
	var out []any
	for i := len(list) - 1; i >= 0; i-- {
		p := list[i]
		if !p.IsVisible() {
			continue
		}
		if v := paint(p); v != nil {
			out = append(out, v)
		}
	}
	switch len(out) {
	case 0:
		return nil
	case 1:
		return out[0]
	}
	return out
}

func paint(p figma.Paint) any {
	opacity := 1.0
	if p.Opacity != nil {
		opacity = *p.Opacity
	}
	switch p.Type {
	case "SOLID":
		if p.Color == nil {
			return nil
		}
		return color(*p.Color, opacity)
	case "GRADIENT_LINEAR", "GRADIENT_RADIAL", "GRADIENT_ANGULAR", "GRADIENT_DIAMOND":
		return gradient(p, opacity)
	case "IMAGE":
		img := map[string]any{"image_ref": p.ImageRef, "scale_mode": strings.ToLower(p.ScaleMode)}
		if p.GifRef != "" {
			img["gif_ref"] = p.GifRef
		}
		return img
	}
	return nil
}

func color(c figma.Color, opacity float64) string {
	a := c.A * opacity
	r, g, b := int(math.Round(c.R*255)), int(math.Round(c.G*255)), int(math.Round(c.B*255))
	if a >= 0.999 {
		return fmt.Sprintf("#%02X%02X%02X", r, g, b)
	}
	return fmt.Sprintf("rgba(%d, %d, %d, %g)", r, g, b, round(a))
}

func gradient(p figma.Paint, opacity float64) string {
	stops := make([]string, 0, len(p.GradientStops))
	for _, s := range p.GradientStops {
		stops = append(stops, fmt.Sprintf("%s %g%%", color(s.Color, opacity), round(s.Position*100)))
	}
	if p.Type == "GRADIENT_LINEAR" {
		angle := 180.0
		if h := p.GradientHandlePositions; len(h) >= 2 {
			angle = math.Mod(math.Atan2(h[1].X-h[0].X, -(h[1].Y-h[0].Y))*180/math.Pi+360, 360)
		}
		return fmt.Sprintf("linear-gradient(%gdeg, %s)", math.Round(angle), strings.Join(stops, ", "))
	}
	kind := map[string]string{"GRADIENT_RADIAL": "radial-gradient", "GRADIENT_ANGULAR": "conic-gradient", "GRADIENT_DIAMOND": "radial-gradient"}[p.Type]
	return fmt.Sprintf("%s(%s)", kind, strings.Join(stops, ", "))
}

func strokesOf(n figma.Node) map[string]any {
	colors := paints(n.Strokes)
	if colors == nil {
		return nil
	}
	out := map[string]any{"paint": colors}
	if w := n.IndividualStrokeWeights; w != nil {
		out["weight"] = strings.Join([]string{fmtNum(w.Top), fmtNum(w.Right), fmtNum(w.Bottom), fmtNum(w.Left)}, " ")
	} else if n.StrokeWeight != nil {
		out["weight"] = fmtNum(*n.StrokeWeight)
	}
	if n.StrokeAlign != "" && n.StrokeAlign != "INSIDE" {
		out["align"] = strings.ToLower(n.StrokeAlign)
	}
	if len(n.StrokeDashes) > 0 {
		out["dashes"] = n.StrokeDashes
	}
	return out
}

func effectsOf(effects []figma.Effect) any {
	var out []string
	for _, e := range effects {
		if !e.IsVisible() {
			continue
		}
		switch e.Type {
		case "DROP_SHADOW", "INNER_SHADOW":
			x, y := 0.0, 0.0
			if e.Offset != nil {
				x, y = e.Offset.X, e.Offset.Y
			}
			c := "#000000"
			if e.Color != nil {
				c = color(*e.Color, 1)
			}
			s := fmt.Sprintf("%s %s %s %s %s", fmtNum(x), fmtNum(y), fmtNum(e.Radius), fmtNum(e.Spread), c)
			if e.Type == "INNER_SHADOW" {
				s = "inset " + s
			}
			out = append(out, s)
		case "LAYER_BLUR":
			out = append(out, "blur("+fmtNum(e.Radius)+")")
		case "BACKGROUND_BLUR":
			out = append(out, "backdrop-blur("+fmtNum(e.Radius)+")")
		}
	}
	switch len(out) {
	case 0:
		return nil
	case 1:
		return out[0]
	}
	return out
}

func radiusOf(n figma.Node) any {
	if r := n.RectangleCornerRadii; len(r) == 4 && !(r[0] == r[1] && r[1] == r[2] && r[2] == r[3]) {
		return strings.Join([]string{fmtNum(r[0]), fmtNum(r[1]), fmtNum(r[2]), fmtNum(r[3])}, " ")
	}
	if n.CornerRadius != nil && *n.CornerRadius != 0 {
		return round(*n.CornerRadius)
	}
	return nil
}

func textStyleOf(s *figma.TypeStyle) map[string]any {
	if s == nil {
		return nil
	}
	out := map[string]any{}
	if s.FontFamily != "" {
		out["font"] = s.FontFamily
	}
	if s.FontWeight != 0 {
		out["weight"] = s.FontWeight
	}
	if s.FontSize != 0 {
		out["size"] = fmtNum(s.FontSize)
	}
	if s.Italic {
		out["italic"] = true
	}
	switch {
	case s.LineHeightUnit == "INTRINSIC_%":
	case s.LineHeightPx != 0:
		out["line_height"] = fmtNum(s.LineHeightPx)
	}
	if s.LetterSpacing != 0 && s.FontSize != 0 {
		out["letter_spacing"] = fmt.Sprintf("%gem", round(s.LetterSpacing/s.FontSize))
	}
	if s.TextAlignHorizontal != "" && s.TextAlignHorizontal != "LEFT" {
		out["align"] = strings.ToLower(s.TextAlignHorizontal)
	}
	if s.TextCase != "" && s.TextCase != "ORIGINAL" {
		out["case"] = strings.ToLower(s.TextCase)
	}
	if s.TextDecoration != "" && s.TextDecoration != "NONE" {
		out["decoration"] = strings.ToLower(s.TextDecoration)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// textOf returns the characters with bold and italic runs (from style
// overrides) marked up as markdown.
func textOf(n figma.Node) string {
	if len(n.CharacterStyleOverrides) == 0 || len(n.StyleOverrideTable) == 0 {
		return n.Characters
	}
	runes := []rune(n.Characters)
	baseWeight, baseItalic := 400.0, false
	if n.Style != nil {
		baseWeight, baseItalic = n.Style.FontWeight, n.Style.Italic
	}
	mark := func(i int) string {
		if i >= len(n.CharacterStyleOverrides) {
			return ""
		}
		o, ok := n.StyleOverrideTable[fmt.Sprint(n.CharacterStyleOverrides[i])]
		if !ok {
			return ""
		}
		m := ""
		if o.FontWeight >= 600 && baseWeight < 600 {
			m += "**"
		}
		if o.Italic && !baseItalic {
			m += "_"
		}
		return m
	}
	var b strings.Builder
	current := ""
	for i, r := range runes {
		m := mark(i)
		if m != current {
			b.WriteString(reverse(current))
			b.WriteString(m)
			current = m
		}
		b.WriteRune(r)
	}
	b.WriteString(reverse(current))
	return b.String()
}

func reverse(marker string) string {
	r := []rune(marker)
	for i, j := 0, len(r)-1; i < j; i, j = i+1, j-1 {
		r[i], r[j] = r[j], r[i]
	}
	return string(r)
}

// CSSColor renders a Figma colour the way node records do (#RRGGBB, or rgba).
func CSSColor(c figma.Color) string { return color(c, 1) }
