package cli

import (
	"context"
	"encoding/json"
	"slices"
	"strings"

	agenterrors "github.com/shhac/lib-agent-output"
	"github.com/spf13/cobra"

	"github.com/shhac/agent-figma/internal/cache"
	"github.com/shhac/agent-figma/internal/cli/shared"
	"github.com/shhac/agent-figma/internal/fetch"
	"github.com/shhac/agent-figma/internal/output"
	"github.com/shhac/agent-figma/internal/simplify"
)

func registerNodes(root *cobra.Command, globals *shared.GlobalFlags) {
	nodes := &cobra.Command{
		Use:   "nodes",
		Short: "Read nodes: simplified properties, raw JSON, or search the cache",
	}
	registerNodesGet(nodes, globals)
	registerNodesFind(nodes, globals)
	registerNodesNotes(nodes, globals)
	root.AddCommand(nodes)
}

func registerNodesGet(parent *cobra.Command, globals *shared.GlobalFlags) {
	var nodeFlags []string
	var depth, maxNodes int
	var view string
	var raw, expand bool
	cmd := &cobra.Command{
		Use:   "get <ref>",
		Short: "Simplified node subtrees, with related nodes listed up front",
		Long: `Prints @file, @styles (shared style values), one record per node in depth-first order,
then @unresolved for ids Figma does not have, @related for nearby nodes (ancestors, the component
set and sibling variants, main components) with what reading them would cost, @pagination when
--max-nodes cut the output, and @budget.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, err := shared.ParseRef(args[0], nodeFlags)
			if err != nil {
				return err
			}
			if err := shared.RequireNodes(ref, "nodes get"); err != nil {
				return err
			}
			views := append(slices.Clone(simplify.Views), viewComponents)
			if !slices.Contains(views, view) {
				return agenterrors.New("--view takes "+strings.Join(views, ", "), agenterrors.FixableByAgent)
			}
			return shared.WithSession(cmd.Context(), globals, ref, func(ctx context.Context, session *fetch.Session) error {
				res, err := session.Nodes(ctx, ref.NodeIDs, depth)
				if err != nil {
					return err
				}
				noteCut(res.Cut)
				if raw {
					return writeRaw(globals, session, res, ref.NodeIDs, depth)
				}
				m := readMapping(globals)
				if view == viewComponents {
					result := simplify.Simplify(res.Store, ref.NodeIDs, simplify.Options{View: simplify.ViewStructure, MaxDepth: depth, ExpandInstances: expand})
					m.annotate(&result, res.Store)
					return writeComponents(globals, session, result.Components, res.Missing)
				}
				result := simplify.Simplify(res.Store, ref.NodeIDs, simplify.Options{View: simplify.View(view), MaxDepth: depth, MaxNodes: maxNodes, ExpandInstances: expand})
				m.annotate(&result, res.Store)
				return writeNodes(globals, session, result, res.Missing, fetch.RelatedTo(res.Store, ref.NodeIDs))
			})
		},
	}
	cmd.Flags().StringArrayVar(&nodeFlags, "node", nil, "Node id(s) (1:2 or 1-2; repeatable, comma-separated); defaults to the link's node-id")
	cmd.Flags().IntVar(&depth, "depth", cache.Full, "Levels below each node to include (-1 for the whole subtree)")
	cmd.Flags().StringVar(&view, "view", string(simplify.ViewAll), "What to include: "+strings.Join(simplify.Views, "|")+"|"+viewComponents+" (only the design-system usage summary)")
	cmd.Flags().BoolVar(&expand, "expand-instances", false, "Show the insides of component instances (by default each is one leaf with its props and texts)")
	cmd.Flags().BoolVar(&raw, "raw", false, "Print Figma's node JSON unchanged, one line per requested node")
	cmd.Flags().IntVar(&maxNodes, "max-nodes", 400, "Most records to print; the rest are listed in @pagination.next_cursor")
	parent.AddCommand(cmd)
}

// writeNodes prints simplified records with the shared metadata lines.
func writeNodes(globals *shared.GlobalFlags, session *fetch.Session, result simplify.Result, missing []string, related []fetch.Related) error {
	stream, err := shared.FileStream(globals, session)
	if err != nil {
		return err
	}
	if len(result.Styles) > 0 {
		stream.Meta(output.MetaStyles, result.Styles)
	}
	if len(result.Components) > 0 {
		stream.Meta(output.MetaComponents, result.Components)
	}
	for _, rec := range result.Records {
		stream.Item(rec)
	}
	for _, id := range missing {
		stream.MetaEach(output.MetaUnresolved, unresolvedNode(id))
	}
	for _, rel := range related {
		stream.MetaEach(output.MetaRelated, rel)
	}
	if len(result.Next) > 0 {
		stream.Meta(output.MetaPagination, output.Pagination{HasMore: true, NextCursor: strings.Join(result.Next, ",")})
	}
	return shared.Finish(stream, session)
}

// viewComponents prints only the @components summary, one record per
// component set or standalone component.
const viewComponents = "components"

func writeComponents(globals *shared.GlobalFlags, session *fetch.Session, uses []simplify.ComponentUse, missing []string) error {
	stream, err := shared.FileStream(globals, session)
	if err != nil {
		return err
	}
	for _, u := range uses {
		stream.Item(u)
	}
	for _, id := range missing {
		stream.MetaEach(output.MetaUnresolved, unresolvedNode(id))
	}
	return shared.Finish(stream, session)
}

func writeRaw(globals *shared.GlobalFlags, session *fetch.Session, res *fetch.NodesResult, ids []string, depth int) error {
	stream, err := shared.FileStream(globals, session)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if tree := rawTree(res.Store, id, depth); tree != nil {
			stream.Item(tree)
		}
	}
	for _, id := range res.Missing {
		stream.MetaEach(output.MetaUnresolved, unresolvedNode(id))
	}
	return shared.Finish(stream, session)
}

// rawTree reassembles Figma's JSON for a node and its stored descendants.
func rawTree(store *cache.NodeStore, id string, depth int) map[string]any {
	stored := store.Nodes[id]
	if stored == nil {
		return nil
	}
	var node map[string]any
	if err := json.Unmarshal(stored.Props, &node); err != nil {
		return nil
	}
	if depth == 0 || stored.KnownDepth == 0 {
		return node
	}
	children := make([]any, 0, len(stored.Children))
	for _, c := range stored.Children {
		next := depth
		if next != cache.Full {
			next--
		}
		if child := rawTree(store, c, next); child != nil {
			children = append(children, child)
		}
	}
	node["children"] = children
	return node
}
