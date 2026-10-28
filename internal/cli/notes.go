package cli

import (
	"context"

	agenterrors "github.com/shhac/lib-agent-output"
	"github.com/spf13/cobra"

	"github.com/shhac/agent-figma/internal/cache"
	"github.com/shhac/agent-figma/internal/cli/shared"
	"github.com/shhac/agent-figma/internal/fetch"
	"github.com/shhac/agent-figma/internal/notes"
)

func registerNodesNotes(parent *cobra.Command, globals *shared.GlobalFlags) {
	var nodeFlags []string
	var noComments bool
	var radius float64
	cmd := &cobra.Command{
		Use:   "notes <ref>",
		Short: "Designer context around a node: canvas and section notes, comments, descriptions, annotations",
		Long: `Collects what designers left around --node: text and sticky notes on the canvas within --radius
(source "canvas", with position and distance), loose text in a containing section ("section"), comments
pinned to the node or inside it, with replies ("comment"; one Tier 2 call, cached), descriptions of the
components it uses ("description"), and Dev Mode annotations ("annotation"). Some notes are for other
designers, some for whoever builds it: read them all, weigh by source. Canvas notes need the node's
siblings cached; @notes_coverage says when they are not.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, err := shared.ParseRef(args[0], nodeFlags)
			if err != nil {
				return err
			}
			if len(ref.NodeIDs) != 1 {
				return agenterrors.New("nodes notes takes exactly one node", agenterrors.FixableByAgent)
			}
			return shared.WithSession(cmd.Context(), globals, ref, func(ctx context.Context, session *fetch.Session) error {
				found, cov, err := collectNotes(ctx, session, ref.NodeIDs[0], radius, !noComments)
				if err != nil {
					return err
				}
				stream, err := shared.FileStream(globals, session)
				if err != nil {
					return err
				}
				for _, n := range found {
					stream.Item(n)
				}
				if !cov.Complete {
					stream.Meta("@notes_coverage", map[string]any{
						"complete": false, "uncached": cov.Uncached,
						"hint": "Notes beside the node may be missing: outline its parent with 'agent-figma file outline <ref> --node " + cov.Uncached[0] + " --depth 1' (one Tier 1 call), then ask again.",
					})
				}
				return shared.Finish(stream, session)
			})
		},
	}
	cmd.Flags().StringArrayVar(&nodeFlags, "node", nil, "The node to gather notes for (defaults to the link's node-id)")
	cmd.Flags().BoolVar(&noComments, "no-comments", false, "Skip comments (saves a Tier 2 call)")
	cmd.Flags().Float64Var(&radius, "radius", notes.DefaultRadius, "How far from the node canvas notes may be, in Figma pixels")
	parent.AddCommand(cmd)
}

func collectNotes(ctx context.Context, session *fetch.Session, id string, radius float64, withComments bool) ([]notes.Note, notes.Coverage, error) {
	res, err := session.Nodes(ctx, []string{id}, cache.Full)
	if err != nil {
		return nil, notes.Coverage{}, err
	}
	if len(res.Missing) > 0 {
		return nil, notes.Coverage{}, agenterrors.New("Figma has no node "+id+" in this file", agenterrors.FixableByAgent)
	}
	store := res.Store
	found, cov := notes.Canvas(store, id, radius)
	found = append(found, notes.Annotations(store, id)...)
	found = append(found, notes.Descriptions(store, id)...)
	if withComments {
		comments, err := session.Comments(ctx)
		if err != nil {
			return nil, cov, err
		}
		found = append(found, notes.Comments(store, id, comments)...)
	}
	return found, cov, nil
}
