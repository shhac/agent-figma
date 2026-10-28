package cli

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/shhac/agent-figma/internal/cache"
	"github.com/shhac/agent-figma/internal/cli/shared"
	"github.com/shhac/agent-figma/internal/output"
	"github.com/shhac/agent-figma/internal/simplify"
)

func registerFile(root *cobra.Command, globals *shared.GlobalFlags) {
	file := &cobra.Command{
		Use:   "file",
		Short: "File-level reads: metadata, outline, catalogue",
	}
	file.AddCommand(&cobra.Command{
		Use:   "meta <ref>",
		Short: "File metadata and current version (Tier 3, cheap)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, err := shared.ParseRef(args[0], nil)
			if err != nil {
				return err
			}
			return shared.WithClient(cmd.Context(), globals, func(ctx context.Context, r *shared.Resolved) error {
				session := shared.NewSession(r, globals, ref)
				meta, err := session.Meta(ctx)
				if err != nil {
					return err
				}
				stream, err := output.NewStream(globals.Format)
				if err != nil {
					return err
				}
				record := map[string]any{
					"key":             ref.FileKey,
					"branch_key":      ref.BranchKey,
					"name":            meta.Name,
					"version":         meta.Version,
					"last_touched_at": meta.LastTouchedAt,
					"editor_type":     meta.EditorType,
					"folder_name":     meta.FolderName,
					"role":            meta.Role,
					"link_access":     meta.LinkAccess,
					"thumbnail_url":   meta.ThumbnailURL,
				}
				if meta.LastTouchedBy != nil {
					record["last_touched_by"] = meta.LastTouchedBy.Handle
				}
				stream.Item(record)
				return shared.Finish(stream, session)
			})
		},
	})
	registerOutline(file, globals)
	registerIndex(file, globals)
	root.AddCommand(file)
}

func registerOutline(parent *cobra.Command, globals *shared.GlobalFlags) {
	var nodes []string
	var depth, maxNodes int
	cmd := &cobra.Command{
		Use:   "outline <ref>",
		Short: "Structure only: id, type, name, size, child count, cache first",
		Long:  "Without --node, lists pages and the frames on them (depth counts from the pages). With --node, outlines that node's subtree. Answers from the cache when it can; otherwise one Tier 1 call.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, err := shared.ParseRef(args[0], nodes)
			if err != nil {
				return err
			}
			return shared.WithClient(cmd.Context(), globals, func(ctx context.Context, r *shared.Resolved) error {
				session := shared.NewSession(r, globals, ref)
				var store *cache.NodeStore
				var roots []string
				var missing []string
				if len(ref.NodeIDs) > 0 {
					res, err := session.Nodes(ctx, ref.NodeIDs, depth)
					if err != nil {
						return err
					}
					store, roots, missing = res.Store, ref.NodeIDs, res.Missing
				} else {
					store, err = session.Document(ctx, depth+1)
					if err != nil {
						return err
					}
					if root := store.Nodes[store.Root]; root != nil {
						roots = root.Children
					}
				}
				result := simplify.Simplify(store, roots, simplify.Options{View: simplify.ViewStructure, MaxDepth: depth, MaxNodes: maxNodes})
				return writeNodes(globals, session, result, missing, nil)
			})
		},
	}
	cmd.Flags().StringArrayVar(&nodes, "node", nil, "Node id(s) to outline (1:2 or 1-2; repeatable, comma-separated)")
	cmd.Flags().IntVar(&depth, "depth", 2, "Levels below each page or node")
	cmd.Flags().IntVar(&maxNodes, "max-nodes", 400, "Most records to print; the rest are listed in @pagination.next_cursor")
	parent.AddCommand(cmd)
}
