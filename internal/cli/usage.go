package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func registerUsageCommand(root *cobra.Command) {
	root.AddCommand(&cobra.Command{
		Use:   "usage",
		Short: "LLM-optimized reference card",
		Run: func(cmd *cobra.Command, args []string) {
			_, _ = fmt.Fprint(cmd.OutOrStdout(), usageText)
		},
	})
}

const usageText = `agent-figma — Figma file, node, and asset CLI for AI agents

STATUS
  Scaffold only; no Figma commands yet.
`
