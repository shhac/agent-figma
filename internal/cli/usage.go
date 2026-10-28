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

SETUP (a human creates the token: Figma > Settings > Security > Personal access tokens)
  agent-figma auth add <profile> --form [--expires YYYY-MM-DD] [--seat full|dev|collab|view] [--plan starter|pro|org|enterprise] [--default]
  agent-figma auth add <profile> --kind plan --form          plan access token (Org/Enterprise admins)
  agent-figma auth update <profile> [--form] [--expires ...] [--seat ...] [--plan ...] [--default]
  agent-figma auth check [profile] [--file <ref>]             verifies the token; shows expiry and observed budget
  agent-figma auth list | default <profile> | remove <profile>
  agent-figma config list | get <key> | set <key> <value> | unset <key> | show | path
    keys: timeout_ms, max_retries, max_wait_s, cache_fresh_s, prefetch (related|off), serve_stale (auto|on|off)

  Recommended read-only token scopes: file_content:read, file_metadata:read, library_content:read,
  library_assets:read, team_library_content:read, file_versions:read, file_comments:read,
  file_dev_resources:read, current_user:read (+ file_variables:read on Enterprise).
  Token precedence: --token, AGENT_FIGMA_TOKEN, the profile's stored token, FIGMA_TOKEN.

REFERENCES
  <ref> is a figma.com link (design, file, proto, board, ... incl. /branch/ links) or a bare file key.
  Node ids accept 1:2 or 1-2 (URL form) and instance chains like I1:2;3:4.
  agent-figma resolve <url>...                                 parse links offline: file_key, branch_key, node_ids

RAW ACCESS
  agent-figma api get <path> [--param k=v]...                  GET any API path, rate-limited like everything else

RATE LIMITS
  Figma meters requests per tier. Tier 1 (file, nodes, image renders) is scarce: 10-20/min on Full/Dev seats,
  only a few per MONTH on Collab/View seats or Starter files. Tier 3 (meta, components, styles) is cheap.
  Short 429s are waited out (--max-wait, default 60s); longer ones fail fast with retry_after_seconds and,
  for low seats, fixable_by:human. Cooldowns are shared across parallel agent-figma processes.

OUTPUT
  NDJSON on stdout (--format json|yaml for one envelope); errors as {error, fixable_by, hint} JSON on stderr.
`
