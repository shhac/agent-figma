package cli

import (
	"strings"

	libcli "github.com/shhac/lib-agent-cli/cli"
	agenterrors "github.com/shhac/lib-agent-output"
	"github.com/spf13/cobra"

	"github.com/shhac/agent-figma/internal/cli/auth"
	"github.com/shhac/agent-figma/internal/cli/shared"
	"github.com/shhac/agent-figma/internal/config"
	"github.com/shhac/agent-figma/internal/output"
)

func newRootCmd(version string) *cobra.Command {
	globals := &shared.GlobalFlags{}

	var root *cobra.Command
	root = libcli.NewRoot(libcli.Options{
		Use:            "agent-figma",
		Short:          "Figma file, node, and asset CLI for AI agents",
		Version:        version,
		Globals:        &globals.Globals,
		DefaultFormat:  output.FormatNDJSON,
		ConfigDefaults: func(*cobra.Command) { applyConfiguredDefaults(root, globals) },
		UnknownHint:    "run 'agent-figma usage' for the command overview",
	})
	// Commands from the shared libraries write to the cobra stream; point it at
	// the same writer as everything else.
	root.SetOut(output.Stdout())

	pf := root.PersistentFlags()
	pf.StringVarP(&globals.Profile, "profile", "p", "", "Profile alias (or AGENT_FIGMA_PROFILE)")
	pf.StringVar(&globals.Token, "token", "", "Figma token override; never printed or persisted")
	pf.StringVar(&globals.BaseURL, "base-url", "", "Figma API base URL override (or AGENT_FIGMA_BASE_URL), for the mock server")
	pf.IntVar(&globals.MaxRetries, "max-retries", config.DefaultMaxRetries, "Retries for transient 5xx and network failures; 429 waits do not count")
	pf.IntVar(&globals.MaxWaitS, "max-wait", config.DefaultMaxWaitS, "Longest rate-limit wait in seconds to sit out before failing")
	pf.BoolVar(&globals.Refresh, "refresh", false, "Re-check the file's version now and refetch if it changed")
	pf.BoolVar(&globals.Offline, "offline", false, "Never call Figma; answer from the cache or fail")
	pf.BoolVar(&globals.NoPrefetch, "no-prefetch", false, "Request only what was asked for, without widening Tier 1 calls")
	_ = pf.MarkHidden("base-url")

	registerUsageCommand(root)
	registerConfig(root, globals)
	auth.Register(root, globals)
	registerResolve(root, globals)
	registerAPI(root, globals)
	registerFile(root, globals)
	registerNodes(root, globals)
	registerLibrary(root, globals)
	registerRender(root, globals)
	registerAssets(root, globals)
	registerReads(root, globals)
	registerCache(root, globals)
	registerDiff(root, globals)

	installUnknownSubcommandHandlers(root)
	registerMCP(root)
	return root
}

func applyConfiguredDefaults(root *cobra.Command, globals *shared.GlobalFlags) {
	d := config.Read().Defaults
	pf := root.PersistentFlags()
	if !pf.Changed("timeout") {
		globals.TimeoutMS = d.EffectiveTimeoutMS()
	}
	if !pf.Changed("max-retries") {
		globals.MaxRetries = d.EffectiveMaxRetries()
	}
	if !pf.Changed("max-wait") {
		globals.MaxWaitS = d.EffectiveMaxWaitS()
	}
}

// installUnknownSubcommandHandlers gives every command group the structured
// unknown-subcommand error the root already has.
func installUnknownSubcommandHandlers(root *cobra.Command) {
	var walk func(cmd *cobra.Command)
	walk = func(cmd *cobra.Command) {
		for _, sub := range cmd.Commands() {
			if !sub.HasSubCommands() {
				continue
			}
			if sub.RunE == nil && sub.Run == nil {
				libcli.HandleUnknownCommand(sub, "run 'agent-figma "+sub.Name()+" --help' to see its subcommands")
			}
			walk(sub)
		}
	}
	walk(root)
}

// Execute builds the root command and runs it via the shared sink, which
// renders any bubbled error as the family's structured JSON on stderr exactly
// once and exits non-zero.
func Execute(version string) {
	libcli.Run(newRootCmd(version))
}

// RunForTest executes one command in-process and returns the exit code the
// binary would have used, with output going through output.SetWritersForTest.
func RunForTest(version string, args []string) int {
	config.ClearCache()
	root := newRootCmd(version)
	root.SetArgs(args)
	root.SetOut(output.Stdout())
	root.SetErr(output.Stderr())
	// Commands that read a token from a pipe find nothing, not the test runner's stdin.
	root.SetIn(strings.NewReader(""))
	if err := root.Execute(); err != nil {
		agenterrors.WriteError(output.Stderr(), err)
		return 1
	}
	return 0
}
