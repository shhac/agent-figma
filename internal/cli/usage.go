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
  Recommended read-only scopes: file_content:read, file_metadata:read, library_content:read, library_assets:read,
  team_library_content:read, file_versions:read, file_comments:read, file_dev_resources:read, current_user:read
  (+ file_variables:read on Enterprise). Token precedence: --token, AGENT_FIGMA_TOKEN, profile, FIGMA_TOKEN.

REFERENCES
  <ref> is a figma.com link (design, file, proto, board, ... incl. /branch/ links) or a bare file key.
  A link's node-id becomes the default --node. Node ids: 1:2, 1-2, or instance chains like I1:2;3:4.
  agent-figma resolve <url>...                         parse links offline: file_key, branch_key, node_ids

START HERE (cheapest first)
  agent-figma file meta <ref>                          Tier 3: name, version, last edit
  agent-figma file index <ref> [--no-outline]          pages, top frames, component sets + variants (with
                                                       thumbnail_url), components, styles
  agent-figma file outline <ref> [--node id] [--depth 2] [--max-nodes 400]   structure only

NODES
  agent-figma nodes get <ref> [--node id...] [--depth N] [--view all|structure|layout|text|visuals] [--raw] [--max-nodes 400]
  agent-figma nodes find <ref> [--name 'button*'] [--type COMPONENT] [--page Screens] [--limit 100]   cache only

IMAGES AND ASSETS
  agent-figma render <ref> --node id... [--as png|jpg|svg|pdf] [--scale 2] [--out dir] [--url-only] [--with-related]
      svg: [--svg-outline-text] [--svg-include-id] [--svg-simplify-stroke]; [--contents-only] [--absolute-bounds]
  agent-figma assets fills <ref> [--node id...] [--out dir]   image fills (Tier 2, whole file map cached)
  agent-figma assets export <ref> --node id... [--out dir]    nodes designers marked for export
  Files land in the cache, named <page>/<node-name>--<id>[@2x].<ext>; each manifest line has path and a
  FileRef "file" (root "cache") for the MCP fs tool.

LIBRARY AND OTHER READS
  agent-figma components|component-sets|styles list <ref>      Tier 3; main file key (branch links are routed)
  agent-figma components|component-sets|styles get <key>...    @unresolved for unknown keys
  agent-figma variables list <ref> [--published]                Enterprise plans only
  agent-figma comments list <ref> [--as-md]
  agent-figma versions list <ref> [--limit 30] [--cursor C]
  agent-figma dev-resources list <ref> [--node id...]
  agent-figma api get <path> [--param k=v]...                   any GET path, rate-limited like everything else

CACHE
  agent-figma cache status [ref] | path | clear <ref> | clear --all
  --refresh re-checks the file version now; --offline never calls Figma; --no-prefetch disables widening.

OUTPUT
  NDJSON on stdout (--format json|yaml for one envelope). Metadata lines:
    @file {key, name, version, stale?, latest_version?}   @styles {id: {name?, value}}   @budget {calls, cache_hits, prefetched}
    @related {rel, id, name, cost}   cost: cached (free) | download (CDN only, no API budget) | tier1
    @unresolved {id, reason, fixable_by, hint}   @pagination {has_more, next_cursor}
  For nodes, next_cursor lists the node ids still to print: pass it back as --node to continue
  (cached, so following it to the end costs no Tier 1 call). svg:true marks vectors best exported as SVG.
  Errors: {error, fixable_by: agent|human|retry, hint, retry_after_seconds?} JSON on stderr, exit 1.

RATE LIMITS
  Figma meters requests per tier, per user and plan. Tier 1 (file, nodes, renders) is scarce: 10-20/min on
  Full/Dev seats, only a few per MONTH on Collab/View seats or Starter files. Tier 3 (meta, library) is cheap.
  Every Tier 1 call is widened with likely-next ids (component sets, sibling variants) and cached per file
  version, so prefer @related entries with cost cached or download. Short 429s are waited out (--max-wait 60s);
  longer ones fail fast with retry_after_seconds, fixable_by:human for low seats. Cooldowns are shared across
  parallel agent-figma processes. On low seats an older cached version answers when the file changed
  (@file.stale); --refresh forces a refetch.
`
