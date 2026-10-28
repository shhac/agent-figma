package fetch

import (
	"math"
	"slices"
	"strings"

	"github.com/shhac/agent-figma/internal/cache"
	"github.com/shhac/agent-figma/internal/figma"
)

// ExportGroup is one render call's worth of designer export settings: a
// single format and scale, with the file suffix each node asked for.
type ExportGroup struct {
	Format   string
	Scale    float64
	IDs      []string
	Suffixes map[string]string
}

// ExportGroups collects the export settings under ids, one group per format
// and scale, since a render call takes a single format and scale. A node with
// two settings of the same format and scale lands in two groups so both
// suffixes are kept.
func ExportGroups(store *cache.NodeStore, ids []string) []ExportGroup {
	var groups []*ExportGroup
	groupFor := func(format string, scale float64, id string) *ExportGroup {
		for _, g := range groups {
			if g.Format == format && g.Scale == scale && !slices.Contains(g.IDs, id) {
				return g
			}
		}
		g := &ExportGroup{Format: format, Scale: scale, Suffixes: map[string]string{}}
		groups = append(groups, g)
		return g
	}
	store.WalkVisible(ids, func(id string, n figma.Node) {
		for _, setting := range n.ExportSettings {
			format := strings.ToLower(setting.Format)
			if !slices.Contains(Formats, format) {
				continue
			}
			g := groupFor(format, exportScale(n, setting), id)
			g.IDs = append(g.IDs, id)
			g.Suffixes[id] = setting.Suffix
		}
	})
	out := make([]ExportGroup, len(groups))
	for i, g := range groups {
		out[i] = *g
	}
	return out
}

// exportScale turns an export constraint into a render scale.
func exportScale(n figma.Node, s figma.ExportSetting) float64 {
	scale := 1.0
	box := n.AbsoluteBoundingBox
	switch s.Constraint.Type {
	case "SCALE":
		scale = s.Constraint.Value
	case "WIDTH":
		if box != nil && box.Width > 0 {
			scale = s.Constraint.Value / box.Width
		}
	case "HEIGHT":
		if box != nil && box.Height > 0 {
			scale = s.Constraint.Value / box.Height
		}
	}
	if scale <= 0 {
		scale = 1
	}
	return math.Min(math.Max(math.Round(scale*1000)/1000, 0.01), 4)
}
