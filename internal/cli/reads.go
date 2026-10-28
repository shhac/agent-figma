package cli

import (
	"context"
	"encoding/json"
	"net/url"
	"slices"
	"strconv"
	"strings"

	agenterrors "github.com/shhac/lib-agent-output"
	"github.com/spf13/cobra"

	"github.com/shhac/agent-figma/internal/cli/shared"
	"github.com/shhac/agent-figma/internal/figmaurl"
	"github.com/shhac/agent-figma/internal/output"
)

// fileRead runs a simple read of one file-scoped endpoint and streams records.
func fileRead(cmd *cobra.Command, globals *shared.GlobalFlags, arg string, nodes []string, run func(ctx context.Context, r *shared.Resolved, ref figmaurl.Ref, stream *output.Stream) error) error {
	ref, err := shared.ParseRef(arg, nodes)
	if err != nil {
		return err
	}
	return shared.WithClient(cmd.Context(), globals, func(ctx context.Context, r *shared.Resolved) error {
		stream, err := output.NewStream(globals.Format)
		if err != nil {
			return err
		}
		if err := run(ctx, r, ref, stream); err != nil {
			return err
		}
		return stream.Close()
	})
}

func decode(raw []byte, v any, what string) error {
	if err := json.Unmarshal(raw, v); err != nil {
		return agenterrors.Wrap(err, agenterrors.FixableByRetry).WithHint("Figma returned an unexpected " + what + " response")
	}
	return nil
}

func registerReads(root *cobra.Command, globals *shared.GlobalFlags) {
	var published bool
	variables := &cobra.Command{Use: "variables", Short: "Variables and collections (Enterprise plans only)"}
	variables.AddCommand(&cobra.Command{
		Use:   "list <ref>",
		Short: "List collections and variables with their values per mode (Tier 2)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return fileRead(cmd, globals, args[0], nil, func(ctx context.Context, r *shared.Resolved, ref figmaurl.Ref, stream *output.Stream) error {
				which := "local"
				if published {
					which = "published"
				}
				raw, err := r.Client.Get(ctx, "/v1/files/"+ref.Key()+"/variables/"+which, nil)
				if err != nil {
					return err
				}
				var resp struct {
					Meta struct {
						Variables           map[string]map[string]any `json:"variables"`
						VariableCollections map[string]map[string]any `json:"variableCollections"`
					} `json:"meta"`
				}
				if err := decode(raw, &resp, "variables"); err != nil {
					return err
				}
				modeNames := map[string]string{}
				for _, id := range sortedAnyKeys(resp.Meta.VariableCollections) {
					c := resp.Meta.VariableCollections[id]
					if modes, ok := c["modes"].([]any); ok {
						for _, m := range modes {
							if mm, ok := m.(map[string]any); ok {
								modeNames[str(mm["modeId"])] = str(mm["name"])
							}
						}
					}
					rec := pick(c, "id", "name", "key", "modes", "defaultModeId", "remote", "hiddenFromPublishing")
					rec["kind"] = "collection"
					stream.Item(rec)
				}
				for _, id := range sortedAnyKeys(resp.Meta.Variables) {
					v := resp.Meta.Variables[id]
					rec := pick(v, "id", "name", "key", "variableCollectionId", "resolvedType", "description", "scopes", "remote", "codeSyntax")
					rec["kind"] = "variable"
					if values, ok := v["valuesByMode"].(map[string]any); ok {
						byName := map[string]any{}
						for mode, val := range values {
							name := modeNames[mode]
							if name == "" {
								name = mode
							}
							byName[name] = val
						}
						rec["values"] = byName
					}
					stream.Item(rec)
				}
				return nil
			})
		},
	})
	variables.PersistentFlags().BoolVar(&published, "published", false, "List variables published from this file instead of local ones")
	root.AddCommand(variables)

	var asMarkdown bool
	comments := &cobra.Command{Use: "comments", Short: "File comments (read-only)"}
	commentsList := &cobra.Command{
		Use:   "list <ref>",
		Short: "List a file's comments, with the node each is pinned to (Tier 2)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return fileRead(cmd, globals, args[0], nil, func(ctx context.Context, r *shared.Resolved, ref figmaurl.Ref, stream *output.Stream) error {
				q := url.Values{}
				if asMarkdown {
					q.Set("as_md", "true")
				}
				raw, err := r.Client.Get(ctx, "/v1/files/"+ref.Key()+"/comments", q)
				if err != nil {
					return err
				}
				var resp struct {
					Comments []map[string]any `json:"comments"`
				}
				if err := decode(raw, &resp, "comments"); err != nil {
					return err
				}
				for _, c := range resp.Comments {
					rec := pick(c, "id", "message", "created_at", "resolved_at", "parent_id", "order_id")
					if u, ok := c["user"].(map[string]any); ok {
						rec["user"] = u["handle"]
					}
					if meta, ok := c["client_meta"].(map[string]any); ok {
						if node := meta["node_id"]; node != nil {
							rec["node_id"] = node
						}
					}
					stream.Item(rec)
				}
				return nil
			})
		},
	}
	commentsList.Flags().BoolVar(&asMarkdown, "as-md", false, "Return comment text as markdown")
	comments.AddCommand(commentsList)
	root.AddCommand(comments)

	var limit int
	var cursor string
	versions := &cobra.Command{Use: "versions", Short: "File version history"}
	versionsList := &cobra.Command{
		Use:   "list <ref>",
		Short: "List versions, newest first (Tier 2, paginated)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return fileRead(cmd, globals, args[0], nil, func(ctx context.Context, r *shared.Resolved, ref figmaurl.Ref, stream *output.Stream) error {
				q := url.Values{"page_size": {strconv.Itoa(limit)}}
				if cursor != "" {
					q.Set("before", cursor)
				}
				raw, err := r.Client.Get(ctx, "/v1/files/"+ref.Key()+"/versions", q)
				if err != nil {
					return err
				}
				var resp struct {
					Versions   []map[string]any `json:"versions"`
					Pagination struct {
						NextPage string `json:"next_page"`
					} `json:"pagination"`
				}
				if err := decode(raw, &resp, "versions"); err != nil {
					return err
				}
				for _, v := range resp.Versions {
					rec := pick(v, "id", "created_at", "label", "description")
					if u, ok := v["user"].(map[string]any); ok {
						rec["user"] = u["handle"]
					}
					stream.Item(rec)
				}
				if next := pageCursor(resp.Pagination.NextPage); next != "" {
					stream.Meta(output.MetaPagination, map[string]any{"has_more": true, "next_cursor": next})
				}
				return nil
			})
		},
	}
	versionsList.Flags().IntVar(&limit, "limit", 30, "Versions per page (max 50)")
	versionsList.Flags().StringVar(&cursor, "cursor", "", "next_cursor from a previous page")
	versions.AddCommand(versionsList)
	root.AddCommand(versions)

	var devNodes []string
	dev := &cobra.Command{Use: "dev-resources", Short: "Dev Mode resources (links attached to nodes)"}
	devList := &cobra.Command{
		Use:   "list <ref>",
		Short: "List dev resources, optionally only for --node (Tier 2; needs the main file key)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return fileRead(cmd, globals, args[0], devNodes, func(ctx context.Context, r *shared.Resolved, ref figmaurl.Ref, stream *output.Stream) error {
				q := url.Values{}
				if len(ref.NodeIDs) > 0 {
					q.Set("node_ids", joinComma(ref.NodeIDs))
				}
				raw, err := r.Client.Get(ctx, "/v1/files/"+ref.FileKey+"/dev_resources", q)
				if err != nil {
					return err
				}
				var resp struct {
					DevResources []map[string]any `json:"dev_resources"`
				}
				if err := decode(raw, &resp, "dev resources"); err != nil {
					return err
				}
				for _, d := range resp.DevResources {
					stream.Item(d)
				}
				return nil
			})
		},
	}
	devList.Flags().StringArrayVar(&devNodes, "node", nil, "Only resources on these node ids")
	dev.AddCommand(devList)
	root.AddCommand(dev)
}

// pick copies the named, non-null fields.
func pick(m map[string]any, keys ...string) map[string]any {
	out := map[string]any{}
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil && v != "" {
			out[k] = v
		}
	}
	return out
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

func sortedAnyKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

func joinComma(ids []string) string { return strings.Join(ids, ",") }

// pageCursor extracts the "before" cursor from Figma's next_page URL.
func pageCursor(next string) string {
	if next == "" {
		return ""
	}
	u, err := url.Parse(next)
	if err != nil {
		return ""
	}
	return u.Query().Get("before")
}
