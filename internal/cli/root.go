package cli

import (
	"github.com/spf13/cobra"

	libcli "github.com/shhac/lib-agent-cli/cli"
	output "github.com/shhac/lib-agent-output"
)

func newRootCmd(version string) *cobra.Command {
	globals := &libcli.Globals{}

	root := libcli.NewRoot(libcli.Options{
		Use:           "agent-figma",
		Short:         "Figma file, node, and asset CLI for AI agents",
		Version:       version,
		Globals:       globals,
		DefaultFormat: output.FormatNDJSON,
		UnknownHint:   "run 'agent-figma usage' for the command overview",
	})

	registerUsageCommand(root)

	return root
}

// Execute builds the root command and runs it via the shared sink, which
// renders any bubbled error as the family's structured JSON on stderr exactly
// once and exits non-zero.
func Execute(version string) {
	libcli.Run(newRootCmd(version))
}
