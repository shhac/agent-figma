package cli

import (
	agenterrors "github.com/shhac/lib-agent-output"
	"github.com/spf13/cobra"

	"github.com/shhac/agent-figma/internal/cli/shared"
	"github.com/shhac/agent-figma/internal/figmaurl"
	"github.com/shhac/agent-figma/internal/output"
)

func registerResolve(root *cobra.Command, globals *shared.GlobalFlags) {
	root.AddCommand(&cobra.Command{
		Use:   "resolve <url-or-key>...",
		Short: "Parse Figma links into file keys and node ids (no API call)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rows := make([]any, 0, len(args))
			for _, arg := range args {
				ref, err := figmaurl.Parse(arg)
				if err != nil {
					return agenterrors.Wrap(err, agenterrors.FixableByAgent).
						WithHint("Pass a figma.com link (design, file, proto, board, ...) or a bare file key.")
				}
				rows = append(rows, map[string]any{
					"input":       arg,
					"file_key":    ref.FileKey,
					"branch_key":  ref.BranchKey,
					"api_key":     ref.Key(),
					"editor_path": ref.EditorPath,
					"node_ids":    ref.NodeIDs,
				})
			}
			return output.WriteList(rows, nil, globals.Format)
		},
	})
}
