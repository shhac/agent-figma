package cli

import (
	"cmp"
	"context"
	"path"
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
		rec := pruned(map[string]any{"id": id, "type": n.Type, "name": n.Name, "label": cmp.Or(label, labels.For(id)), "page": pageName, "parent": stored.Parent})
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
