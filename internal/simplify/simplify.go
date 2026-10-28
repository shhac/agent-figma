// Package simplify turns cached Figma nodes into compact, LLM-shaped records:
// invisible and auto-named noise dropped, layout in flexbox terms, paints as
// CSS-ish strings, vector-only groups collapsed into one SVG leaf, and style
// values used more than once hoisted into a shared table.
package simplify

import (
	"maps"
	"regexp"
	"slices"
	"strings"

	"github.com/shhac/agent-figma/internal/cache"
	"github.com/shhac/agent-figma/internal/figma"
)

type View string

const (
	ViewAll       View = "all"
	ViewStructure View = "structure"
	ViewLayout    View = "layout"
	ViewText      View = "text"
	ViewVisuals   View = "visuals"
)

var Views = []string{string(ViewAll), string(ViewStructure), string(ViewLayout), string(ViewText), string(ViewVisuals)}

type Options struct {
	View View
	// MaxDepth limits levels below each root (cache.Full for no limit).
	MaxDepth int
	// MaxNodes caps emitted records across all roots (0 for no cap).
	MaxNodes int
	// ExpandInstances emits the insides of component instances. By default
	// an instance is a leaf: its component, props, and texts.
	ExpandInstances bool
	// InlineStyles keeps every style value on its record instead of hoisting
	// shared ones into the table, for comparing records across outputs.
	InlineStyles bool
}

// Record is one simplified node.
type Record struct {
	ID     string `json:"id"`
	Parent string `json:"parent,omitempty"`
	Depth  int    `json:"depth"`
	Type   string `json:"type"`
	Name   string `json:"name,omitempty"`
	// Label is the most prominent heading under a generically named
	// container, so "Card" reads as what it shows.
	Label     string        `json:"label,omitempty"`
	Size      []float64     `json:"size,omitempty"`
	Pos       []float64     `json:"pos,omitempty"`
	Layout    any           `json:"layout,omitempty"`
	Fills     any           `json:"fills,omitempty"`
	Strokes   any           `json:"strokes,omitempty"`
	Effects   any           `json:"effects,omitempty"`
	Radius    any           `json:"radius,omitempty"`
	Opacity   *float64      `json:"opacity,omitempty"`
	Text      string        `json:"text,omitempty"`
	TextStyle any           `json:"text_style,omitempty"`
	Component *ComponentRef `json:"component,omitempty"`
	// Code is the mapped code component for an instance, when the
	// repository maps its component.
	Code     *CodeRef            `json:"code,omitempty"`
	Props    map[string]any      `json:"props,omitempty"`
	Variants map[string][]string `json:"variant_props,omitempty"`
	Export   bool                `json:"export,omitempty"`
	// SVG marks a vector, or a container of only vectors, best exported as
	// one SVG; its children are not listed.
	SVG bool `json:"svg,omitempty"`
	// Texts are the visible texts inside a folded instance.
	Texts []string `json:"texts,omitempty"`
	// Instance marks an instance whose insides were folded.
	Instance bool `json:"instance,omitempty"`
	Children int  `json:"children,omitempty"`
	// Truncated marks a node whose visible children were not emitted.
	Truncated bool `json:"truncated,omitempty"`
}

type ComponentRef struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
	Set  string `json:"set,omitempty"`
}

// StyleEntry is a shared style table value; Name is set for named Figma styles.
type StyleEntry struct {
	Name  string `json:"name,omitempty"`
	Value any    `json:"value"`
}

type Result struct {
	Records []Record
	Styles  map[string]StyleEntry
	// Next are the ids to request to continue: nodes MaxNodes left out, and
	// nodes whose children were never fetched.
	Next []string
	// Components summarises the component instances emitted.
	Components []ComponentUse
}

// Simplify renders the subtrees under roots from the store.
func Simplify(store *cache.NodeStore, roots []string, opts Options) Result {
	w := &walker{store: store, opts: opts, styles: newStyleTable(store), roots: map[string]bool{}, uses: newComponentUses(store)}
	for _, r := range roots {
		w.roots[r] = true
	}
	included := w.plan(roots)
	w.labels = NewLabeler(store)
	w.labels.CountNames(slices.Collect(maps.Keys(included)))
	for _, root := range roots {
		if _, known := w.node(root); known && !included[root] {
			w.next = append(w.next, root)
		}
		w.emit(root, 0, included)
	}
	return Result{Records: w.records, Styles: w.styles.finish(w.records, opts.InlineStyles), Next: w.next, Components: w.uses.list()}
}

type walker struct {
	store   *cache.NodeStore
	opts    Options
	styles  *styleTable
	records []Record
	next    []string
	cache   map[string]figma.Node
	svg     map[string]bool
	roots   map[string]bool
	uses    *componentUses
	labels  *Labeler
}

// folds reports an instance shown as a leaf: any instance, unless asked to
// expand them or it is a requested root (whose insides were asked for).
func (w *walker) folds(id string, n figma.Node) bool {
	return n.Type == "INSTANCE" && !w.opts.ExpandInstances && !w.roots[id]
}

func (w *walker) node(id string) (figma.Node, bool) {
	if w.cache == nil {
		w.cache = map[string]figma.Node{}
	}
	if n, ok := w.cache[id]; ok {
		return n, true
	}
	n, ok := w.store.Node(id)
	if ok {
		w.cache[id] = n
	}
	return n, ok
}

// visibleChildren lists a node's stored children that are visible.
func (w *walker) visibleChildren(id string) []string {
	stored := w.store.Nodes[id]
	if stored == nil {
		return nil
	}
	var out []string
	for _, c := range stored.Children {
		if n, ok := w.node(c); ok && n.IsVisible() {
			out = append(out, c)
		}
	}
	return out
}

// plan picks which nodes to emit, breadth first, so a node cap keeps the
// top of the tree rather than one deep branch.
func (w *walker) plan(roots []string) map[string]bool {
	included := map[string]bool{}
	type item struct {
		id    string
		depth int
	}
	queue := []item{}
	for _, r := range roots {
		if _, ok := w.node(r); ok {
			queue = append(queue, item{r, 0})
		}
	}
	count := 0
	for len(queue) > 0 {
		it := queue[0]
		queue = queue[1:]
		if w.opts.MaxNodes > 0 && count >= w.opts.MaxNodes {
			continue
		}
		included[it.id] = true
		count++
		n, _ := w.node(it.id)
		if w.collapsesToSVG(it.id) || w.folds(it.id, n) || (w.opts.MaxDepth != cache.Full && it.depth >= w.opts.MaxDepth) {
			continue
		}
		for _, c := range w.visibleChildren(it.id) {
			queue = append(queue, item{c, it.depth + 1})
		}
	}
	return included
}

func (w *walker) emit(id string, depth int, included map[string]bool) {
	if !included[id] {
		return
	}
	n, ok := w.node(id)
	if !ok {
		return
	}
	stored := w.store.Nodes[id]
	svg := w.collapsesToSVG(id)
	rec := w.record(n, stored, depth, svg)
	if n.Type == "INSTANCE" {
		w.uses.add(id, n)
	}
	if svg {
		w.records = append(w.records, rec)
		return
	}
	children := w.visibleChildren(id)
	if w.folds(id, n) {
		rec.Children = len(children)
		rec.Instance = true
		rec.Texts = w.texts(id, maxInstanceTexts)
		w.records = append(w.records, rec)
		return
	}
	rec.Children = len(children)
	depthCut := w.opts.MaxDepth != cache.Full && depth >= w.opts.MaxDepth
	// Children cut by MaxNodes, or never fetched (Figma refused the full
	// subtree as too large), are where a continuation picks up. Children cut
	// by MaxDepth were asked to be left out.
	childrenUnknown := stored.KnownDepth == 0 && len(children) == 0 && hasChildrenType(n.Type)
	if childrenUnknown && !depthCut {
		w.next = append(w.next, id)
	}
	for _, c := range children {
		if !included[c] && !depthCut {
			w.next = append(w.next, c)
		}
	}
	rec.Truncated = childrenUnknown || slices.ContainsFunc(children, func(c string) bool { return !included[c] })
	w.records = append(w.records, rec)
	for _, c := range children {
		w.emit(c, depth+1, included)
	}
}

const (
	textPreviewRunes = 80
	maxInstanceTexts = 8
)

// texts collects the visible text under id, depth first, previewed.
func (w *walker) texts(id string, limit int) []string {
	var out []string
	var walk func(id string)
	walk = func(id string) {
		for _, c := range w.visibleChildren(id) {
			if len(out) >= limit {
				return
			}
			n, _ := w.node(c)
			if n.Type == "TEXT" {
				if t := preview(n.Characters, textPreviewRunes); t != "" {
					out = append(out, t)
				}
				continue
			}
			walk(c)
		}
	}
	walk(id)
	return out
}

// Preview is the first line of a text, cut to the outline's preview length.
func Preview(s string) string { return preview(s, textPreviewRunes) }

// preview is the first line of s, cut to max runes with an ellipsis.
func preview(s string, max int) string {
	line, _, cut := strings.Cut(strings.TrimSpace(s), "\n")
	runes := []rune(line)
	if len(runes) > max {
		return string(runes[:max-1]) + "…"
	}
	if cut {
		return line + "…"
	}
	return line
}

func hasChildrenType(t string) bool {
	switch t {
	case "DOCUMENT", "CANVAS", "FRAME", "GROUP", "COMPONENT", "COMPONENT_SET", "INSTANCE", "SECTION", "BOOLEAN_OPERATION":
		return true
	}
	return false
}

var autoName = regexp.MustCompile(`^(Frame|Rectangle|Ellipse|Line|Vector|Group|Polygon|Star|Text|Image|Union|Subtract|Intersect|Exclude|Section|Slice|Arrow|Boolean)\s+\d+$`)

func displayName(n figma.Node) string {
	if n.Type == "TEXT" || autoName.MatchString(n.Name) {
		return ""
	}
	return n.Name
}

func (w *walker) record(n figma.Node, stored *cache.StoredNode, depth int, svg bool) Record {
	rec := Record{ID: n.ID, Parent: stored.Parent, Depth: depth, Type: n.Type, Name: displayName(n), Label: w.labels.For(n.ID)}
	rec.SVG = svg || n.Type == "VECTOR"
	if box := n.AbsoluteBoundingBox; box != nil {
		rec.Size = []float64{round(box.Width), round(box.Height)}
	}
	view := w.opts.View
	want := func(v View) bool { return view == ViewAll || view == v }
	parent, hasParent := w.node(stored.Parent)
	if want(ViewLayout) && depth > 0 && hasParent {
		rec.Pos = position(n, parent)
	}
	if want(ViewLayout) {
		if layout := layoutOf(n, parent, hasParent && depth > 0); layout != nil {
			rec.Layout = w.styles.use("layout", layout, "")
		}
	}
	if want(ViewVisuals) || (view == ViewText && n.Type == "TEXT") {
		if fills := paints(n.Fills); fills != nil {
			rec.Fills = w.styles.use("fill", fills, w.styles.named(n, "fill", "fills"))
		}
	}
	if want(ViewVisuals) {
		if strokes := strokesOf(n); strokes != nil {
			rec.Strokes = w.styles.use("stroke", strokes, w.styles.named(n, "stroke", "strokes"))
		}
		if effects := effectsOf(n.Effects); effects != nil {
			rec.Effects = w.styles.use("effect", effects, w.styles.named(n, "effect", "effects"))
		}
		rec.Radius = radiusOf(n)
		if n.Opacity != nil && *n.Opacity < 1 {
			o := round(*n.Opacity)
			rec.Opacity = &o
		}
	}
	if view == ViewStructure && n.Type == "TEXT" {
		// TEXT layer names are dropped as noise, so the outline shows what
		// the text says instead.
		rec.Text = preview(n.Characters, textPreviewRunes)
	}
	if want(ViewText) && n.Type == "TEXT" {
		rec.Text = textOf(n)
		if style := textStyleOf(n.Style); style != nil {
			rec.TextStyle = w.styles.use("text", style, w.styles.named(n, "text"))
		}
	}
	if view == ViewAll {
		w.componentInfo(&rec, n, stored)
		rec.Export = len(n.ExportSettings) > 0
	}
	return rec
}
