package cli

import (
	"context"
	"net/url"
	"strings"

	agenterrors "github.com/shhac/lib-agent-output"
	"github.com/spf13/cobra"

	"github.com/shhac/agent-figma/internal/cli/shared"
	"github.com/shhac/agent-figma/internal/output"
)

func registerAPI(root *cobra.Command, globals *shared.GlobalFlags) {
	var params []string
	apiCmd := &cobra.Command{
		Use:   "api",
		Short: "Raw Figma REST API access (GET only)",
	}
	get := &cobra.Command{
		Use:   "get <path>",
		Short: "GET any Figma API path, through the same rate limiter and retries",
		Long:  "GET any Figma API path (e.g. /v1/files/<key>/versions). Requests are classified into Figma's rate-limit tiers like every other command; Tier 1 paths (/v1/files/<key>, /nodes, /v1/images) spend the scarce budget and bypass the cache.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := args[0]
			if strings.Contains(path, "://") {
				return agenterrors.New("pass a path like /v1/me, not a full URL", agenterrors.FixableByAgent)
			}
			query := url.Values{}
			for _, p := range params {
				key, value, ok := strings.Cut(p, "=")
				if !ok {
					return agenterrors.New("--param takes key=value, got "+p, agenterrors.FixableByAgent)
				}
				query.Add(key, value)
			}
			if before, rawQuery, ok := strings.Cut(path, "?"); ok {
				parsed, err := url.ParseQuery(rawQuery)
				if err != nil {
					return agenterrors.Wrap(err, agenterrors.FixableByAgent)
				}
				for k, vs := range parsed {
					for _, v := range vs {
						query.Add(k, v)
					}
				}
				path = before
			}
			return shared.WithClient(cmd.Context(), globals, func(ctx context.Context, r *shared.Resolved) error {
				raw, err := r.Client.Get(ctx, path, query)
				if err != nil {
					return err
				}
				return output.WriteItem(raw, globals.Format)
			})
		},
	}
	get.Flags().StringArrayVar(&params, "param", nil, "Query parameter as key=value (repeatable)")
	apiCmd.AddCommand(get)
	root.AddCommand(apiCmd)
}
