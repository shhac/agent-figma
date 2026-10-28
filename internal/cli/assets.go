package cli

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/shhac/agent-figma/internal/cache"
	"github.com/shhac/agent-figma/internal/cli/shared"
	"github.com/shhac/agent-figma/internal/fetch"
	"github.com/shhac/agent-figma/internal/output"
)

func registerAssets(root *cobra.Command, globals *shared.GlobalFlags) {
	assets := &cobra.Command{Use: "assets", Short: "Download image fills and designer-marked exports"}
	var fillNodes []string
	var fillOut string
	fills := &cobra.Command{
		Use:   "fills <ref>",
		Short: "Download the image fills used under --node (or every fill in the file)",
		Long:  "The fill map comes from one Tier 2 call and covers the whole file; it is cached. With --node, only fills used in those subtrees are downloaded (their nodes are read cache-first).",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, err := shared.ParseRef(args[0], fillNodes)
			if err != nil {
				return err
			}
			if err := checkOut(fillOut); err != nil {
				return err
			}
			return shared.WithClient(cmd.Context(), globals, func(ctx context.Context, r *shared.Resolved) error {
				session := shared.NewSession(r, globals, ref)
				var uses []fetch.FillUse
				var missingNodes []string
				if len(ref.NodeIDs) > 0 {
					res, err := session.Nodes(ctx, ref.NodeIDs, cache.Full)
					if err != nil {
						return err
					}
					uses, missingNodes = fetch.FillRefs(res.Store, ref.NodeIDs), res.Missing
				}
				urls, err := session.Fills(ctx)
				if err != nil {
					return err
				}
				if len(ref.NodeIDs) == 0 {
					for _, imageRef := range sortedKeys(urls) {
						uses = append(uses, fetch.FillUse{ImageRef: imageRef})
					}
				}
				downloaded, missing := session.DownloadFills(ctx, uses, urls)
				stream, err := output.NewStream(globals.Format)
				if err != nil {
					return err
				}
				stream.Meta(output.MetaFile, session.FileInfo)
				for _, f := range downloaded {
					rec := pruned(map[string]any{
						"image_ref": f.ImageRef, "nodes": f.Nodes, "scale_mode": f.ScaleMode, "format": strings.TrimPrefix(filepath.Ext(f.Path), "."),
						"mime": f.MIME, "source": f.Source,
					})
					rec["bytes"], rec["downloaded"] = f.Bytes, f.Downloaded
					fileFields(rec, f.Path)
					if fillOut != "" {
						if err := copyOut(rec, f.Path, filepath.Join(fillOut, "fills", filepath.Base(f.Path))); err != nil {
							return err
						}
					}
					stream.Item(rec)
				}
				for _, id := range missingNodes {
					stream.MetaEach(output.MetaUnresolved, unresolvedNode(id))
				}
				for _, u := range missing {
					stream.MetaEach(output.MetaUnresolved, u)
				}
				return shared.Finish(stream, session)
			})
		},
	}
	fills.Flags().StringArrayVar(&fillNodes, "node", nil, "Only fills used under these node ids")
	fills.Flags().StringVar(&fillOut, "out", "", "Also copy fills into <dir>/fills/")
	assets.AddCommand(fills)

	var exportNodes []string
	var exportOut string
	export := &cobra.Command{
		Use:   "export <ref>",
		Short: "Render every node under --node that designers marked with export settings",
		Long:  "Reads the subtrees cache-first, collects export settings (format, scale or width/height constraint, suffix), and spends one render call per distinct format and scale.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, err := shared.ParseRef(args[0], exportNodes)
			if err != nil {
				return err
			}
			if err := shared.RequireNodes(ref, "assets export"); err != nil {
				return err
			}
			if err := checkOut(exportOut); err != nil {
				return err
			}
			return shared.WithClient(cmd.Context(), globals, func(ctx context.Context, r *shared.Resolved) error {
				session := shared.NewSession(r, globals, ref)
				res, err := session.Nodes(ctx, ref.NodeIDs, cache.Full)
				if err != nil {
					return err
				}
				stream, err := output.NewStream(globals.Format)
				if err != nil {
					return err
				}
				stream.Meta(output.MetaFile, session.FileInfo)
				for _, group := range fetch.ExportGroups(res.Store, ref.NodeIDs) {
					rendered, err := session.Render(ctx, fetch.RenderRequest{
						IDs: group.IDs, Format: group.Format, Scale: group.Scale, Suffixes: group.Suffixes,
						SVGOutlineText: true, SVGSimplifyStroke: true, ContentsOnly: true, Source: "export",
					})
					if err != nil {
						return err
					}
					if err := writeImages(stream, rendered.Images, exportOut, false); err != nil {
						return err
					}
					for _, u := range rendered.Unresolved {
						stream.MetaEach(output.MetaUnresolved, u)
					}
				}
				for _, id := range res.Missing {
					stream.MetaEach(output.MetaUnresolved, unresolvedNode(id))
				}
				return shared.Finish(stream, session)
			})
		},
	}
	export.Flags().StringArrayVar(&exportNodes, "node", nil, "Subtrees to search for export settings")
	export.Flags().StringVar(&exportOut, "out", "", "Also copy exports into this directory, under <page>/")
	assets.AddCommand(export)
	root.AddCommand(assets)
}
