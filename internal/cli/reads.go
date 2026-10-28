package cli

import (
	"context"
	"net/url"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/shhac/agent-figma/internal/api"
	"github.com/shhac/agent-figma/internal/cli/shared"
	"github.com/shhac/agent-figma/internal/fetch"
	"github.com/shhac/agent-figma/internal/figma"
	"github.com/shhac/agent-figma/internal/output"
)

// fileRead runs a read of one file-scoped endpoint that is not cached: the
// session supplies key routing and the @budget line.
func fileRead(cmd *cobra.Command, globals *shared.GlobalFlags, arg string, nodes []string, run func(ctx context.Context, s *fetch.Session, stream *output.Stream) error) error {
	ref, err := shared.ParseRef(arg, nodes)
	if err != nil {
		return err
	}
	return shared.WithSession(cmd.Context(), globals, ref, func(ctx context.Context, session *fetch.Session) error {
		stream, err := output.NewStream(globals.Format)
		if err != nil {
			return err
		}
		if err := run(ctx, session, stream); err != nil {
			return err
		}
		return shared.Finish(stream, session)
	})
}

func registerReads(root *cobra.Command, globals *shared.GlobalFlags) {
	registerVariables(root, globals)
	registerComments(root, globals)
	registerVersions(root, globals)
	registerDevResources(root, globals)
}

func registerComments(root *cobra.Command, globals *shared.GlobalFlags) {
	var asMarkdown bool
	group := &cobra.Command{Use: "comments", Short: "File comments (read-only)"}
	list := &cobra.Command{
		Use:   "list <ref>",
		Short: "List a file's comments, with the node each is pinned to (Tier 2)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return fileRead(cmd, globals, args[0], nil, func(ctx context.Context, s *fetch.Session, stream *output.Stream) error {
				q := url.Values{}
				if asMarkdown {
					q.Set("as_md", "true")
				}
				raw, err := s.Client.Get(ctx, "/v1/files/"+s.Key()+"/comments", q)
				if err != nil {
					return err
				}
				resp, err := api.Decode[figma.CommentsResponse](raw, "comments")
				if err != nil {
					return err
				}
				for _, c := range resp.Comments {
					stream.Item(pruned(map[string]any{
						"id": c.ID, "message": c.Message, "created_at": c.CreatedAt, "resolved_at": c.ResolvedAt,
						"parent_id": c.ParentID, "order_id": c.OrderID, "user": c.User.Handle, "node_id": c.ClientMeta.NodeID,
					}))
				}
				return nil
			})
		},
	}
	list.Flags().BoolVar(&asMarkdown, "as-md", false, "Return comment text as markdown")
	group.AddCommand(list)
	root.AddCommand(group)
}

func registerVersions(root *cobra.Command, globals *shared.GlobalFlags) {
	var limit int
	var cursor string
	group := &cobra.Command{Use: "versions", Short: "File version history"}
	list := &cobra.Command{
		Use:   "list <ref>",
		Short: "List versions, newest first (Tier 2, paginated)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return fileRead(cmd, globals, args[0], nil, func(ctx context.Context, s *fetch.Session, stream *output.Stream) error {
				q := url.Values{"page_size": {strconv.Itoa(limit)}}
				if cursor != "" {
					q.Set("before", cursor)
				}
				raw, err := s.Client.Get(ctx, "/v1/files/"+s.Key()+"/versions", q)
				if err != nil {
					return err
				}
				resp, err := api.Decode[struct {
					Versions []struct {
						ID          string     `json:"id"`
						CreatedAt   string     `json:"created_at"`
						Label       string     `json:"label"`
						Description string     `json:"description"`
						User        figma.User `json:"user"`
					} `json:"versions"`
					Pagination struct {
						NextPage string `json:"next_page"`
					} `json:"pagination"`
				}](raw, "versions")
				if err != nil {
					return err
				}
				for _, v := range resp.Versions {
					stream.Item(pruned(map[string]any{"id": v.ID, "created_at": v.CreatedAt, "label": v.Label, "description": v.Description, "user": v.User.Handle}))
				}
				if next := pageCursor(resp.Pagination.NextPage); next != "" {
					stream.Meta(output.MetaPagination, output.Pagination{HasMore: true, NextCursor: next})
				}
				return nil
			})
		},
	}
	list.Flags().IntVar(&limit, "limit", 30, "Versions per page (max 50)")
	list.Flags().StringVar(&cursor, "cursor", "", "next_cursor from a previous page")
	group.AddCommand(list)
	root.AddCommand(group)
}

func registerDevResources(root *cobra.Command, globals *shared.GlobalFlags) {
	var nodes []string
	group := &cobra.Command{Use: "dev-resources", Short: "Dev Mode resources (links attached to nodes)"}
	list := &cobra.Command{
		Use:   "list <ref>",
		Short: "List dev resources, optionally only for --node (Tier 2; needs the main file key)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return fileRead(cmd, globals, args[0], nodes, func(ctx context.Context, s *fetch.Session, stream *output.Stream) error {
				q := url.Values{}
				if len(s.Ref.NodeIDs) > 0 {
					q.Set("node_ids", strings.Join(s.Ref.NodeIDs, ","))
				}
				raw, err := s.Client.Get(ctx, "/v1/files/"+s.MainKey()+"/dev_resources", q)
				if err != nil {
					return err
				}
				resp, err := api.Decode[struct {
					DevResources []map[string]any `json:"dev_resources"`
				}](raw, "dev resources")
				if err != nil {
					return err
				}
				for _, d := range resp.DevResources {
					stream.Item(d)
				}
				return nil
			})
		},
	}
	list.Flags().StringArrayVar(&nodes, "node", nil, "Only resources on these node ids")
	group.AddCommand(list)
	root.AddCommand(group)
}

// pageCursor extracts the "before" cursor from Figma's next_page URL.
func pageCursor(next string) string {
	u, err := url.Parse(next)
	if next == "" || err != nil {
		return ""
	}
	return u.Query().Get("before")
}
