package cli

import (
	libcli "github.com/shhac/lib-agent-cli/cli"
	"github.com/spf13/cobra"

	"github.com/shhac/agent-figma/internal/cli/shared"
	"github.com/shhac/agent-figma/internal/config"
	"github.com/shhac/agent-figma/internal/output"
)

func registerConfig(root *cobra.Command, globals *shared.GlobalFlags) {
	cfg := libcli.ConfigCommand(&globals.Globals, configKeys())
	cfg.Short = "Inspect and update non-secret agent-figma config"
	cfg.AddCommand(&cobra.Command{
		Use:   "path",
		Short: "Print the config file path",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return output.WriteItem(map[string]any{"config_path": config.ConfigPath()}, globals.Format)
		},
	})
	cfg.AddCommand(&cobra.Command{
		Use:   "show",
		Short: "Show the whole non-secret config",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return output.WriteItem(config.Read(), globals.Format)
		},
	})
	root.AddCommand(cfg)
}

func configKeys() []libcli.ConfigKey {
	b := libcli.ConfigBinding[config.Config]{
		Read:   func() (config.Config, error) { return *config.Read(), nil },
		Update: config.Update,
	}
	defaults := func(c *config.Config) *config.Defaults { return &c.Defaults }
	return []libcli.ConfigKey{
		libcli.OptionalIntKey(b, "timeout_ms", "Command timeout in milliseconds (default 60000)", func(c *config.Config) **int { return &defaults(c).TimeoutMS }, 0, 3600000),
		libcli.OptionalIntKey(b, "max_retries", "Retries for transient 5xx and network failures (default 2; 429 waits do not count)", func(c *config.Config) **int { return &defaults(c).MaxRetries }, 0, 10),
		libcli.OptionalIntKey(b, "max_wait_s", "Longest rate-limit wait to sit out before failing (default 60)", func(c *config.Config) **int { return &defaults(c).MaxWaitS }, 0, 3600),
		libcli.OptionalIntKey(b, "cache_fresh_s", "Seconds a file's cached version is trusted before re-checking (default 300)", func(c *config.Config) **int { return &defaults(c).CacheFreshS }, 0, 30*86400),
		libcli.OneOfKey(b, "prefetch", "Widen Tier 1 calls with likely-needed ids: related (default) or off", func(c *config.Config) *string { return &defaults(c).Prefetch }, config.PrefetchModes),
		libcli.OneOfKey(b, "serve_stale", "Answer from an older cached version when the file changed: auto (default; on for low seats and Starter files), on, off", func(c *config.Config) *string { return &defaults(c).ServeStale }, config.ServeStaleModes),
	}
}
