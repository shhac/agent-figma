package cli

import (
	"context"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/shhac/agent-figma/internal/cache"
	"github.com/shhac/agent-figma/internal/cli/shared"
	"github.com/shhac/agent-figma/internal/fetch"
	"github.com/shhac/agent-figma/internal/figma"
	"github.com/shhac/agent-figma/internal/output"
	"github.com/shhac/agent-figma/internal/simplify"
)

func registerIndex(parent *cobra.Command, globals *shared.GlobalFlags) {
	var noOutline bool
	cmd := &cobra.Command{
		Use:   "index <ref>",
		Short: "Catalogue a file up front: pages, top-level frames, component sets with variants, components, styles",
		Long: `Builds a catalogue from cheap Tier 3 calls (meta, published components, component sets, styles)
plus the page/frame outline, which costs one Tier 1 call unless it is already cached (--no-outline skips it).
Published library entries include thumbnail_url, so variants can be looked at without spending a render.
Unpublished components appear only once their nodes are cached.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, err := shared.ParseRef(args[0], nil)
			if err != nil {
				return err
			}
			return shared.WithSession(cmd.Context(), globals, ref, func(ctx context.Context, session *fetch.Session) error {
				if _, err := session.CurrentVersion(ctx); err != nil {
					return err
				}
				components, err := session.Library(ctx, fetch.KindComponents)
				if err != nil {
					return err
				}
				sets, err := session.Library(ctx, fetch.KindComponentSets)
				if err != nil {
					return err
				}
				styles, err := session.Library(ctx, fetch.KindStyles)
				if err != nil {
					return err
				}
				var store *cache.NodeStore
				if !noOutline {
					if store, err = session.Document(ctx, outlineDocumentDepth); err != nil {
						return err
					}
				}
				stream, err := shared.FileStream(globals, session)
				if err != nil {
					return err
				}
				if store != nil {
					writeOutlineEntries(stream, store)
				}
				variants := map[string][]map[string]any{}
				var standalone []map[string]any
				for _, c := range components {
					if f := c.ContainingFrame; f != nil && f.ContainingComponentSet != nil {
						setID := f.ContainingComponentSet.NodeID
						variants[setID] = append(variants[setID], map[string]any{
							"node_id": c.NodeID, "key": c.Key, "props": simplify.VariantProps(c.Name), "thumbnail_url": c.ThumbnailURL,
						})
						continue
					}
					rec := libraryRecord(c)
					rec["kind"] = "component"
					standalone = append(standalone, rec)
				}
				for _, s := range sets {
					rec := libraryRecord(s)
					rec["kind"] = "component_set"
					rec["variants"] = variants[s.NodeID]
					stream.Item(rec)
				}
				for _, rec := range standalone {
					stream.Item(rec)
				}
				if store != nil {
					writeLocalComponents(stream, store, components, sets)
				}
				for _, s := range styles {
					rec := libraryRecord(s)
					rec["kind"] = "style"
					stream.Item(rec)
				}
				return shared.Finish(stream, session)
			})
		},
	}
	cmd.Flags().BoolVar(&noOutline, "no-outline", false, "Skip the page/frame outline (avoids a Tier 1 call when it is not cached)")
	parent.AddCommand(cmd)
}

func writeOutlineEntries(stream *output.Stream, store *cache.NodeStore) {
	root := store.Nodes[store.Root]
	if root == nil {
		return
	}
	for _, pageID := range root.Children {
		page, ok := store.Node(pageID)
		if !ok {
			continue
		}
		pageStored := store.Nodes[pageID]
		stream.Item(map[string]any{"kind": "page", "id": pageID, "name": page.Name, "frames": len(pageStored.Children)})
		for _, frameID := range pageStored.Children {
			frame, ok := store.Node(frameID)
			if !ok || !frame.IsVisible() {
				continue
			}
			rec := map[string]any{"kind": "frame", "id": frameID, "type": frame.Type, "name": frame.Name, "page": page.Name}
			if frame.AbsoluteBoundingBox != nil {
				rec["size"] = []float64{frame.AbsoluteBoundingBox.Width, frame.AbsoluteBoundingBox.Height}
			}
			if fs := store.Nodes[frameID]; fs != nil && fs.KnownDepth != 0 {
				rec["children"] = len(fs.Children)
			}
			stream.Item(rec)
		}
	}
}

// writeLocalComponents lists component sets and components the node store
// knows about that are not published, so unpublished files still get a catalogue.
func writeLocalComponents(stream *output.Stream, store *cache.NodeStore, published, publishedSets []figma.PublishedComponent) {
	known := map[string]bool{}
	for _, c := range published {
		known[c.NodeID] = true
	}
	for _, s := range publishedSets {
		known[s.NodeID] = true
	}
	for _, id := range sortedKeys(store.ComponentSets) {
		set := store.ComponentSets[id]
		if known[id] || set.Remote {
			continue
		}
		rec := map[string]any{"kind": "component_set", "node_id": id, "name": set.Name, "published": false}
		var vs []map[string]any
		for cid, c := range store.Components {
			if c.ComponentSetID == id {
				known[cid] = true
				vs = append(vs, map[string]any{"kind": "component", "node_id": cid, "name": c.Name, "props": simplify.VariantProps(c.Name)})
			}
		}
		slices.SortFunc(vs, func(a, b map[string]any) int { return strings.Compare(a["node_id"].(string), b["node_id"].(string)) })
		rec["variants"] = vs
		stream.Item(rec)
	}
	for _, id := range sortedKeys(store.Components) {
		c := store.Components[id]
		if known[id] || c.Remote || c.ComponentSetID != "" {
			continue
		}
		stream.Item(map[string]any{"kind": "component", "node_id": id, "name": c.Name, "published": false})
	}
}
