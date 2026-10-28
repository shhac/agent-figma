package cli

import (
	"context"
	"encoding/json"
	"path"
	"slices"
	"strings"

	agenterrors "github.com/shhac/lib-agent-output"
	"github.com/spf13/cobra"

	"github.com/shhac/agent-figma/internal/cache"
	"github.com/shhac/agent-figma/internal/cli/shared"
	"github.com/shhac/agent-figma/internal/fetch"
	"github.com/shhac/agent-figma/internal/figma"
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
				m, err := loadMapping(globals)
				if err != nil {
					return err
				}
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

// noteCut warns when Figma refused whole subtrees as too large, so what is
// shown stops at a depth limit rather than at the real leaves.
func noteCut(cut []string) {
	if len(cut) == 0 {
		return
	}
	output.Notice("Figma refused the full subtree of "+strings.Join(cut, ", ")+" as too large; showing a depth-limited tree",
		"Records marked truncated have more children; request them with 'nodes get --node <id>'.")
}

func unresolvedNode(id string) fetch.Unresolved {
	return fetch.Unresolved{
		ID:        id,
		Reason:    "Figma has no node " + id + " in this file",
		FixableBy: string(agenterrors.FixableByAgent),
		Hint:      "Check the id with 'agent-figma file outline <ref>'; prototype and FigJam links can use different ids than the design file.",
	}
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

func registerNodesFind(parent *cobra.Command, globals *shared.GlobalFlags) {
	var q findQuery
	var limit int
	cmd := &cobra.Command{
		Use:   "find <ref>",
		Short: "Search cached nodes by name, type, or page (no Tier 1 call when anything is cached)",
		Long:  "Searches every node cached for the file's current version. If nothing is cached yet, first fetches the page/frame outline (one Tier 1 call). @coverage lists frames whose contents are not cached, so a miss there is not proof of absence; deepen them with 'nodes get --node <id>'.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, err := shared.ParseRef(args[0], nil)
			if err != nil {
				return err
			}
			if q == (findQuery{}) {
				return agenterrors.New("nodes find needs --name, --type, or --page", agenterrors.FixableByAgent)
			}
			return shared.WithSession(cmd.Context(), globals, ref, func(ctx context.Context, session *fetch.Session) error {
				store, err := session.Searchable(ctx)
				if err != nil {
					return err
				}
				found := findNodes(store, q, limit)
				stream, err := shared.FileStream(globals, session)
				if err != nil {
					return err
				}
				for _, rec := range found.records {
					stream.Item(rec)
				}
				stream.Meta("@coverage", map[string]any{"searched": len(store.Nodes), "matches": found.matches, "incomplete": firstN(found.incomplete, 50)})
				if found.matches > limit {
					stream.Meta(output.MetaPagination, output.Pagination{HasMore: true})
				}
				return shared.Finish(stream, session)
			})
		},
	}
	cmd.Flags().StringVar(&q.name, "name", "", "Name glob or substring, case-insensitive (e.g. 'button*')")
	cmd.Flags().StringVar(&q.nodeType, "type", "", "Node type, e.g. FRAME, COMPONENT, COMPONENT_SET, INSTANCE, TEXT")
	cmd.Flags().StringVar(&q.page, "page", "", "Only nodes on the page with this name")
	cmd.Flags().IntVar(&limit, "limit", 100, "Most matches to print")
	parent.AddCommand(cmd)
}

type findQuery struct{ name, nodeType, page string }

func (q findQuery) match(n figma.Node, pageName, label string) bool {
	if q.nodeType != "" && !strings.EqualFold(n.Type, q.nodeType) {
		return false
	}
	if q.page != "" && !strings.EqualFold(pageName, q.page) {
		return false
	}
	if q.name == "" {
		return true
	}
	pattern := strings.ToLower(q.name)
	for _, candidate := range []string{n.Name, label} {
		name := strings.ToLower(candidate)
		if name == "" {
			continue
		}
		if globbed, _ := path.Match(pattern, name); globbed || strings.Contains(name, pattern) {
			return true
		}
	}
	return false
}

// containerTypes are the nodes whose uncached contents make a search incomplete.
var containerTypes = map[string]bool{"CANVAS": true, "FRAME": true, "SECTION": true, "COMPONENT_SET": true}

type findResult struct {
	records    []map[string]any
	matches    int
	incomplete []string
}

// findNodes searches the store, keeping the first limit matches and counting
// the rest, and lists containers whose contents are not cached.
func findNodes(store *cache.NodeStore, q findQuery, limit int) findResult {
	res := findResult{incomplete: []string{}}
	labels := simplify.NewLabeler(store)
	for _, id := range sortedKeys(store.Nodes) {
		stored := store.Nodes[id]
		n, ok := store.Node(id)
		if !ok {
			continue
		}
		if stored.KnownDepth == 0 && containerTypes[n.Type] {
			res.incomplete = append(res.incomplete, id)
		}
		pageName := ""
		if p, ok := store.Node(stored.Page); ok {
			pageName = p.Name
		}
		label := ""
		if q.name != "" {
			label = labels.For(id)
		}
		if !q.match(n, pageName, label) {
			continue
		}
		res.matches++
		if res.matches > limit {
			continue
		}
		rec := pruned(map[string]any{"id": id, "type": n.Type, "name": n.Name, "label": cmpOr(label, labels.For(id)), "page": pageName, "parent": stored.Parent})
		if b := n.AbsoluteBoundingBox; b != nil {
			rec["size"] = []float64{b.Width, b.Height}
		}
		res.records = append(res.records, rec)
	}
	return res
}

func firstN(ids []string, n int) []string {
	if len(ids) > n {
		return ids[:n]
	}
	return ids
}
