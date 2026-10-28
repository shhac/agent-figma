package cli

import (
	"cmp"
	"context"

	agenterrors "github.com/shhac/lib-agent-output"
	"github.com/spf13/cobra"

	"github.com/shhac/agent-figma/internal/cache"
	"github.com/shhac/agent-figma/internal/cli/shared"
	"github.com/shhac/agent-figma/internal/diff"
	"github.com/shhac/agent-figma/internal/fetch"
	"github.com/shhac/agent-figma/internal/output"
)

const maxDiffRenders = 20

func registerDiff(root *cobra.Command, globals *shared.GlobalFlags) {
	var nodes []string
	var from, to string
	var render bool
	var maxChanges int
	cmd := &cobra.Command{
		Use:   "diff <ref>",
		Short: "What changed between two versions of a node subtree (usually one Tier 1 call)",
		Long: `Compares --node subtrees (or, without --node, every top-level frame both versions have cached)
between --from and --to. --to defaults to the current version, re-checked now; --from defaults to the
newest older version in the cache, so after any earlier read "what changed?" costs at most one Tier 1
call. An older --from (see 'versions list') that is not cached is fetched with ?version=.

Prints one record per change (added, removed, moved, changed with per-field from/to), then @diff with
counts and the frames containing changes. --render adds before/after images of those frames, one
render call per version.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, err := shared.ParseRef(args[0], nodes)
			if err != nil {
				return err
			}
			flags := *globals
			if to == "" {
				flags.Refresh = true
			}
			return shared.WithSession(cmd.Context(), &flags, ref, func(ctx context.Context, session *fetch.Session) error {
				return runDiff(ctx, &flags, session, ref.NodeIDs, from, to, render, maxChanges)
			})
		},
	}
	cmd.Flags().StringArrayVar(&nodes, "node", nil, "Subtrees to compare (defaults to the link's node-id, else every top-level frame cached at both versions)")
	cmd.Flags().StringVar(&from, "from", "", "Version id to compare from (default: newest older cached version)")
	cmd.Flags().StringVar(&to, "to", "", "Version id to compare to (default: current)")
	cmd.Flags().BoolVar(&render, "render", false, "Render the changed frames at both versions")
	cmd.Flags().IntVar(&maxChanges, "max-changes", 200, "Most change records to print")
	root.AddCommand(cmd)
}

func runDiff(ctx context.Context, flags *shared.GlobalFlags, session *fetch.Session, ids []string, from, to string, render bool, maxChanges int) error {
	if len(ids) == 0 {
		frames, err := defaultDiffRoots(ctx, session, from, to)
		if err != nil {
			return err
		}
		ids = frames
	}
	after, err := diffSide(ctx, session, ids, to)
	if err != nil {
		return err
	}
	if from == "" {
		if from = session.PreviousVersion(after.Version, ids); from == "" {
			return noBaseline()
		}
	}
	before, err := session.NodesAt(ctx, ids, from)
	if err != nil {
		return err
	}
	return writeDiff(ctx, flags, session, before, after, ids, render, maxChanges)
}

// defaultDiffRoots are the top-level frames held in full at the baseline
// version: "what changed in what I looked at before".
func defaultDiffRoots(ctx context.Context, session *fetch.Session, from, to string) ([]string, error) {
	current := to
	if current == "" {
		v, err := session.CurrentVersion(ctx)
		if err != nil {
			return nil, err
		}
		current = v
	}
	baseline := from
	if baseline == "" {
		for _, v := range session.File.Versions() {
			if v != current {
				baseline = v
				break
			}
		}
	}
	if baseline == "" {
		return nil, noBaseline()
	}
	store, err := session.File.LoadNodes(baseline)
	if err != nil {
		return nil, noBaseline()
	}
	var frames []string
	for _, id := range sortedKeys(store.Nodes) {
		parent, ok := store.Node(store.Nodes[id].Parent)
		if ok && parent.Type == "CANVAS" && store.Has(id, cache.Full) {
			frames = append(frames, id)
		}
	}
	if len(frames) == 0 {
		return nil, agenterrors.New("no top-level frame is cached in full at the earlier version", agenterrors.FixableByAgent).
			WithHint("Pass --node <frame id> to compare one area; pair it with --from <version id> if no earlier read cached it.")
	}
	return frames, nil
}

func diffSide(ctx context.Context, session *fetch.Session, ids []string, version string) (*cache.NodeStore, error) {
	if version != "" {
		return session.NodesAt(ctx, ids, version)
	}
	res, err := session.Nodes(ctx, ids, cache.Full)
	if err != nil {
		return nil, err
	}
	if len(res.Missing) > 0 {
		return nil, agenterrors.New("the current version has no node "+res.Missing[0], agenterrors.FixableByAgent).
			WithHint("It may have been deleted; compare its parent instead with --node <parent id>.")
	}
	return res.Store, nil
}

func noBaseline() error {
	return agenterrors.New("no earlier version of these nodes is cached to compare with", agenterrors.FixableByAgent).
		WithHint("Pass --from <version id> from 'agent-figma versions list <ref>' (one more Tier 1 call), or read the nodes now so a later diff has a baseline.")
}

func writeDiff(ctx context.Context, flags *shared.GlobalFlags, session *fetch.Session, before, after *cache.NodeStore, roots []string, render bool, maxChanges int) error {
	changes, sum := diff.Compare(before, after, roots)
	sum.From, sum.To = before.Version, after.Version
	var pairs []map[string]any
	if render && len(sum.Frames) > 0 {
		var err error
		if pairs, err = renderPairs(ctx, session, sum, before.Version, after.Version); err != nil {
			return err
		}
	}
	stream, err := shared.FileStream(flags, session)
	if err != nil {
		return err
	}
	for i, c := range changes {
		if i == maxChanges {
			stream.Meta(output.MetaPagination, output.Pagination{HasMore: true})
			break
		}
		stream.Item(c)
	}
	stream.Meta("@diff", sum)
	for _, p := range pairs {
		stream.MetaEach("@render", p)
	}
	return shared.Finish(stream, session)
}

// renderPairs renders the changed frames at both versions, one batched call
// each, skipping frames that do not exist at a version.
func renderPairs(ctx context.Context, session *fetch.Session, sum diff.Summary, from, to string) ([]map[string]any, error) {
	ids := sum.FrameIDs(maxDiffRenders)
	paths := map[string]map[string]any{}
	for _, side := range []struct{ key, version string }{{"before", from}, {"after", to}} {
		res, err := session.Render(ctx, withVersion(fetch.DefaultRender(ids, "png", 1), side.version))
		if err != nil {
			return nil, err
		}
		for _, img := range res.Images {
			if paths[img.ID] == nil {
				paths[img.ID] = pruned(map[string]any{"id": img.ID, "name": cmp.Or(img.Label, img.Name)})
			}
			rec := map[string]any{}
			fileFields(rec, img.Path)
			paths[img.ID][side.key] = rec
		}
	}
	var out []map[string]any
	for _, id := range ids {
		if p := paths[id]; p != nil {
			out = append(out, p)
		}
	}
	return out, nil
}

func withVersion(req fetch.RenderRequest, version string) fetch.RenderRequest {
	req.Version = version
	return req
}
