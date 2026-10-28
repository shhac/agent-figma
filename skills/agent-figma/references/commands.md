# Commands (reference index)

Every command at a glance, with its cost and where the detail lives. Exact
flags: `agent-figma <command> --help`. `<ref>` is a figma.com link or a bare
file key; `--node` takes `1:2` or `1-2`, repeatable or comma-separated, and
defaults to the link's `node-id`.

**Global flags** (every command): `--profile/-p <alias>`, `--format/-f
json|yaml|jsonl` (default NDJSON), `--refresh`, `--offline`, `--no-prefetch`,
`--max-wait <s>` (default 60), `--max-retries <n>` (default 2),
`--timeout/-t <ms>`, `--debug/-d`, `--color auto|always|never`,
`--components-file <path>` (the [component-to-code mapping](components-to-code.md);
default: the nearest `.agent-figma/components.json`, or `AGENT_FIGMA_COMPONENTS`).
`--token` exists but is for humans only.

| Command | Cost | Reference |
|---|---|---|
| `resolve <url-or-key>...` | none (offline parse) | [library-and-other-reads.md](library-and-other-reads.md#links-and-keys) |
| `file meta <ref>` | Tier 3 | [library-and-other-reads.md](library-and-other-reads.md#file-meta) |
| `file index <ref> [--no-outline]` | Tier 3 ×3-4, + 1 Tier 1 unless the outline is cached (it caches what a default `file outline` needs) | [library-and-other-reads.md](library-and-other-reads.md#file-index) |
| `file outline <ref> [--node id] [--depth N] [--max-nodes N]` | cache first, else 1 Tier 1 | [reading-nodes.md](reading-nodes.md#file-outline) |
| `nodes get <ref> [--node id...] [--depth N] [--view V] [--raw] [--max-nodes N] [--expand-instances]` | cache first, else 1 Tier 1 per call (up to 4 if Figma refuses the subtree as too large); following a `--max-nodes` cursor is free | [reading-nodes.md](reading-nodes.md) |
| `nodes find <ref> [--name glob] [--type T] [--page name] [--limit N]` | cache only (1 Tier 1 if nothing is cached); `--name` matches names and labels | [reading-nodes.md](reading-nodes.md#nodes-find) |
| `nodes notes <ref> --node id [--no-comments] [--radius 400]` | node cache first (else 1 Tier 1), + 1 Tier 2 for comments (cached) | [building-and-verifying.md](building-and-verifying.md#anything-the-designer-said-about-this--nodes-notes) |
| `render <ref> --node id... [--as png\|jpg\|svg\|pdf] [--scale N] [--with-related] [--url-only] [--out dir]` | cache first, else 1 Tier 1 per batch | [rendering-and-assets.md](rendering-and-assets.md) |
| `assets fills <ref> [--node id...] [--out dir]` | 1 Tier 2 (whole file, cached) | [rendering-and-assets.md](rendering-and-assets.md#assets-fills) |
| `assets export <ref> --node id... [--out dir]` | 1 Tier 1 per distinct format+scale | [rendering-and-assets.md](rendering-and-assets.md#assets-export) |
| `components list <ref>` · `components get <key>...` | Tier 3 | [library-and-other-reads.md](library-and-other-reads.md#components-component-sets-styles) |
| `components map status <ref> [--node id]` | cache first, like `nodes get` | [components-to-code.md](components-to-code.md#status-what-is-used-what-is-mapped) |
| `components map add (--set-key K\|--key K\|--name N) --component C [--import P] [--prop F=c]... [--value P.f=c]... [--ignore P]... [--notes T]` | none (writes the mapping file) | [components-to-code.md](components-to-code.md#add-write-an-entry) |
| `components map suggest <ref> [--node id] [--src dir] [--write]` | cache first, like `nodes get` | [components-to-code.md](components-to-code.md#suggest-propose-entries-from-the-source-tree) |
| `component-sets list <ref>` · `component-sets get <key>...` | Tier 3 | same |
| `styles list <ref>` · `styles get <key>...` | Tier 3 | same |
| `variables list <ref> [--published]` | Tier 2, Enterprise only | [library-and-other-reads.md](library-and-other-reads.md#variables) |
| `comments list <ref> [--as-md]` | Tier 2 | [library-and-other-reads.md](library-and-other-reads.md#comments) |
| `versions list <ref> [--limit N] [--cursor C]` | Tier 2 | [library-and-other-reads.md](library-and-other-reads.md#versions) |
| `dev-resources list <ref> [--node id...]` | Tier 2 | [library-and-other-reads.md](library-and-other-reads.md#dev-resources) |
| `api get <path> [--param k=v]...` | tier of the path; Tier 1 paths bypass the cache | [library-and-other-reads.md](library-and-other-reads.md#raw-api) |
| `auth add\|update\|check\|list\|default\|remove` | `check` is Tier 3 | [setup-and-auth.md](setup-and-auth.md) |
| `cache status [ref]` · `cache path` · `cache clear [ref] [--all]` | none | [rate-limits-and-cache.md](rate-limits-and-cache.md#cache) |
| `config list\|get\|set\|unset\|show\|path` | none | [rate-limits-and-cache.md](rate-limits-and-cache.md#config-keys) |
| `usage` | none | built-in reference card |
| `mcp` | none | runs the command tree as an MCP server; `agent-figma mcp usage` |

Output contract, meta lines, and the rate-limit mental model are in
[../SKILL.md](../SKILL.md). Error handling: [errors.md](errors.md).

## Building, tracking changes, verifying

| Command | Cost | Detail |
|---|---|---|
| `handoff <ref> --node id [--out dir] [--scale 2]` | ≤3 Tier 1 + ≤2 Tier 2 (fills, comments), cached parts free | [building-and-verifying.md](building-and-verifying.md#build-this-ui-in-my-app--handoff) |
| `diff <ref> [--node id...] [--from v] [--to v] [--render] [--max-changes 200]` | usually 1 Tier 1 (+1 with an uncached `--from`, +2 with `--render`) | [building-and-verifying.md](building-and-verifying.md#the-designer-updated-the-designs--what-changed--diff) |
| `compare <ref> --node id --image shot.png [--scale] [--threshold 24] [--out]` | ≤1 Tier 1 render (+1 if the node is not cached) | [building-and-verifying.md](building-and-verifying.md#does-my-component-match-the-design--compare) |

