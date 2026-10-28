package cli

import (
	"context"

	libcli "github.com/shhac/lib-agent-cli/cli"
	"github.com/spf13/cobra"

	"github.com/shhac/agent-figma/internal/api"
	"github.com/shhac/agent-figma/internal/cli/shared"
	"github.com/shhac/agent-figma/internal/fetch"
	"github.com/shhac/agent-figma/internal/figma"
	"github.com/shhac/agent-figma/internal/output"
)

// libraryGroups are the published-library command groups and their endpoints.
var libraryGroups = []struct {
	use, kind, single, short string
}{
	{"components", fetch.KindComponents, "/v1/components/", "Published components (Tier 3)"},
	{"component-sets", fetch.KindComponentSets, "/v1/component_sets/", "Published component sets (Tier 3)"},
	{"styles", fetch.KindStyles, "/v1/styles/", "Published styles (Tier 3)"},
}

func registerLibrary(root *cobra.Command, globals *shared.GlobalFlags) {
	for _, g := range libraryGroups {
		group := &cobra.Command{Use: g.use, Short: g.short}
		group.AddCommand(&cobra.Command{
			Use:   "list <ref>",
			Short: "List the file's published " + g.use + " (needs the main file key, not a branch)",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				ref, err := shared.ParseRef(args[0], nil)
				if err != nil {
					return err
				}
				return shared.WithClient(cmd.Context(), globals, func(ctx context.Context, r *shared.Resolved) error {
					session := shared.NewSession(r, globals, ref)
					list, err := session.Library(ctx, g.kind)
					if err != nil {
						return err
					}
					stream, err := output.NewStream(globals.Format)
					if err != nil {
						return err
					}
					for _, c := range list {
						stream.Item(libraryRecord(c))
					}
					return shared.Finish(stream, session)
				})
			},
		})
		group.AddCommand(&cobra.Command{
			Use:   "get <key>...",
			Short: "Look up published " + g.use + " by key (one line per key, @unresolved for misses)",
			Args:  cobra.MinimumNArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				return shared.WithClient(cmd.Context(), globals, func(ctx context.Context, r *shared.Resolved) error {
					return libcli.EntityGet(output.Stdout(), globals.Format, args, func(key string) (any, error) {
						raw, err := r.Client.Get(ctx, g.single+key, nil)
						if err != nil {
							return nil, err
						}
						resp, err := api.Decode[struct {
							Meta figma.PublishedComponent `json:"meta"`
						}](raw, g.use)
						if err != nil {
							return nil, err
						}
						return libraryRecord(resp.Meta), nil
					})
				})
			},
		})
		root.AddCommand(group)
	}
}

func libraryRecord(c figma.PublishedComponent) map[string]any {
	rec := map[string]any{
		"key":           c.Key,
		"node_id":       c.NodeID,
		"name":          c.Name,
		"description":   c.Description,
		"thumbnail_url": c.ThumbnailURL,
		"updated_at":    c.UpdatedAt,
	}
	if c.StyleType != "" {
		rec["style_type"] = c.StyleType
	}
	if c.FileKey != "" {
		rec["file_key"] = c.FileKey
	}
	if f := c.ContainingFrame; f != nil {
		rec["page"] = f.PageName
		rec["frame"] = f.Name
		if s := f.ContainingComponentSet; s != nil {
			rec["set"] = s.Name
			rec["set_node_id"] = s.NodeID
		}
	}
	for k, v := range rec {
		if v == "" {
			delete(rec, k)
		}
	}
	return rec
}
