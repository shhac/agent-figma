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
  Settings nudges: when a 429 reports a seat or plan that contradicts the profile's --seat/--plan, or Figma
  rejects a token before its recorded expiry, the error hint, a stderr notice, and auth check/list
  suggestions [{reason, command}] carry the exact 'agent-figma auth update <profile> ...' to run. Never
  applied automatically: relay the command to the human.
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
  agent-figma nodes get <ref> [--node id...] [--depth N] [--view all|structure|layout|text|visuals|components] [--raw] [--max-nodes 400] [--expand-instances]
      Component instances are leaves by default (component, props, texts); @components summarises which
      design-system components and variants are used, and --view components prints only that summary.
      Generically named containers (no name, Card/Container/Frame/Group/..., or a name 3+ printed nodes
      share) get "label": their most prominent heading text. Labels also name render/handoff files.
  agent-figma nodes find <ref> [--name 'button*'] [--type COMPONENT] [--page Screens] [--limit 100]   cache only;
      --name matches layer names and labels
  agent-figma nodes notes <ref> --node id [--no-comments] [--radius 400]   what designers left around a node:
      {source: canvas|section|comment|description|annotation, text, node_id, name, position?, distance?,
      author?, created_at?, resolved?, replies?}; comments are 1 Tier 2 call; @notes_coverage when the
      node's siblings are not cached (its hint names the file outline call that fixes it)

IMAGES AND ASSETS
  agent-figma render <ref> --node id... [--as png|jpg|svg|pdf] [--scale 2] [--out dir] [--url-only] [--with-related]
      svg: [--svg-outline-text] [--svg-include-id] [--svg-simplify-stroke]; [--contents-only] [--absolute-bounds]
  agent-figma assets fills <ref> [--node id...] [--out dir]   image fills (Tier 2, whole file map cached)
  agent-figma assets export <ref> --node id... [--out dir]    nodes designers marked for export
  Files land in the cache, named <page>/<node-name>--<id>[@2x].<ext>; each manifest line has path and a
  FileRef "file" (root "cache") for the MCP fs tool.

BUILD, TRACK CHANGES, VERIFY
  agent-figma handoff <ref> --node id [--out dir] [--scale 2]     one folder to build from: screenshot.png, tree.ndjson,
      components.json (with code mappings), styles.json, icons/*.svg (one per component), images/*, notes.json,
      manifest.json (<=3 Tier 1 + <=2 Tier 2: fills, comments)
  agent-figma diff <ref> [--node id...] [--from version] [--to version] [--render]   what changed between versions:
      added/removed/moved/changed records with per-field from/to and a path of ancestor names (labels for
      generic ones), @diff summary by frame; baseline defaults to the
      newest older cached version (usually 1 Tier 1 call); --render adds before/after images of changed frames
  agent-figma compare <ref> --node id --image app.png [--scale auto] [--threshold 24]   where a screenshot differs:
      match_percent, regions attributed to design nodes, diff_image; scale inferred from the screenshot

COMPONENTS TO CODE (checked in, shared by everyone in the repo)
  <repo-root>/.agent-figma/components.json, found by walking up from the working directory
  (--components-file or AGENT_FIGMA_COMPONENTS override). Keyed by published set_key (variant sets) or key
  (standalone components), the same for every user, file, and token; name is the fallback for unpublished ones.
  agent-figma components map status <ref> [--node id]         components used, mapped or not, and the file in use
  agent-figma components map add (--set-key K | --key K | --name N) --component Name [--import path]
      [--prop Figma=code]... [--value Prop.figma=code]... [--ignore Prop]... [--notes text]   replaces any entry
      with the same key
  agent-figma components map suggest <ref> [--node id] [--src dir] [--write]   match names against exported
      code components (.tsx .ts .jsx .js .vue .svelte); --write adds exact matches only
  Once the file exists: @components entries carry mapped, code {component, import, notes?}, and per variant
  code_props; instance records carry code {component, import, props}; handoff's components.json too.

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
    @components [{set|component, set_key|key, count, remote?, mapped?, code?, variants: [{props, code_props?, count, nodes}]}]
    @notes_coverage {complete, uncached, hint}   @components_file {path, exists, entries | scanned, exports, written}
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
