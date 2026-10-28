# agent-figma initial design

## Goal

A Go CLI that lets an LLM read Figma design files cheaply and safely:

- resolve any Figma link to a file key and node ids without spending API budget
- outline a file or a node subtree, and read nodes in a compact, LLM-shaped form
- render nodes to png/jpg/svg/pdf and download them to disk, with a manifest
- download image fills and designer-marked exports
- read the library surface (components, component sets, styles), variables, comments, versions, and dev resources
- list related things (variants, children, ancestors, cached renders) up front, so an agent does not have to explore to find them

v1 is read-only. Writes (posting comments, etc.) need their own design section and a `--yes` gate first.

All identifiers in this repo are synthetic (see `AGENTS.md`). Examples below use the file key `FAKEFILEKEY0000000000000`.

## The constraint: Tier 1 is scarce

Figma rate-limits by **request**, per endpoint tier, with budgets that depend on the token owner's seat type and on the plan that owns the file being read (rate-limit docs, in effect since 2025-11-17):

| Tier | Endpoints | Dev/Full seat: Pro / Org / Ent | View/Collab seat (any plan), or Starter |
|---|---|---|---|
| 1 | `GET /v1/files/:key`, `/v1/files/:key/nodes`, `/v1/images/:key` | 10 / 15 / 20 per min | up to ~6–20 per **month** |
| 2 | image fills, comments, versions, variables, dev resources, folders/projects, webhooks | 25 / 50 / 100 per min | up to 5 per min |
| 3 | file `/meta`, components, component sets, styles, `/v1/me` | 50 / 100 / 150 per min | up to 10 per min |

Budgets are a leaky bucket per user per plan (per app for OAuth, per token for plan access tokens). A 429 carries `Retry-After` (seconds), `X-Figma-Plan-Tier` (`enterprise|org|pro|starter|student`), `X-Figma-Rate-Limit-Type` (`low` = Collab/View seat, `high` = Full/Dev seat), and `X-Figma-Upgrade-Link`. No headers on successful responses report remaining budget.

Figma's own guidance is to batch node ids into one request and cache. Everything below follows from spending a Tier 1 call only when the cache cannot answer, and making each Tier 1 call carry as much likely-needed data as is safe.

## Auth and profiles

Profiles follow the family pattern (`agent-posthog`, `agent-stripe`):

- `config.json` under `xdg.ConfigDir("agent-figma")` holds non-secret profile metadata and defaults.
- `credentials.json` (0600) is the index; the token itself lives in the OS keychain (`app.paulie.agent-figma`) via `lib-agent-cli/creds`, falling back to the index file when no keychain is available (`AGENT_FIGMA_NO_KEYCHAIN` / `LIB_AGENT_NO_KEYCHAIN`).
- `--form` prompts through the native dialog (`lib-agent-cli/dialog`) so the token never transits chat or argv.

### Token kinds

- **Personal access token (v1 default).** Sent as `X-Figma-Token`. Created in Figma under Settings → Security → Personal access tokens. Maximum lifetime is 90 days, so `auth add` records an optional `--expires YYYY-MM-DD` and every command emits a `notice` on stderr when fewer than 7 days remain; `auth check` reports `expires_in_days`.
- **Plan access token.** Same header, lifetime up to 365 days. Org/Enterprise admins create them; they cannot call `/v1/me`, so `auth check` for `--kind plan` verifies with a Tier 3 call against a file the caller names (`auth check <profile> --file <ref>`) instead.
- **OAuth (later).** Bring-your-own private OAuth app, loopback redirect with PKCE (as `agent-notion` does), `Authorization: Bearer`. Deferred: needs an app registered per organization and loopback redirects are not confirmed in Figma's docs. Its benefit is a separate rate budget per app and refresh tokens.

Recommended PAT scopes (read-only v1): `file_content:read`, `file_metadata:read`, `library_content:read`, `library_assets:read`, `team_library_content:read`, `file_versions:read`, `file_comments:read`, `file_dev_resources:read`, `current_user:read`, plus `file_variables:read` on Enterprise. Not the deprecated `files:read`.

### Token resolution

`--token` flag (or `--token -` for stdin, via `creds.ReadSecret`) → `AGENT_FIGMA_TOKEN` → selected profile's stored token → `FIGMA_TOKEN`. `FIGMA_TOKEN` is deliberately last (unlike `agent-posthog`, where the generic variable precedes the profile): it is commonly exported for other Figma tools, and must not silently override a profile the user configured here. Profile selection: `--profile/-p` → `AGENT_FIGMA_PROFILE` → `default_profile`. The token is never printed, logged, or persisted outside the credential backend, and is scrubbed from `--debug` output and error bodies.

### Seat and plan

`/v1/me` does not report seat type, and the plan that matters is the file owner's, not the user's. So:

- `auth add/update --seat full|dev|collab|view` and `--plan starter|pro|org|enterprise` are optional declarations (default `unknown`).
- Every 429 records what Figma reports: `rate_limit_type` per profile, `plan_tier` per file key, in the state store. Both declarations and observations are kept; neither is required.
- The effective seat class is the declaration if set, otherwise the observation, otherwise `high` (assume Full/Dev). The effective plan for a file is the observation for that file, otherwise the profile declaration, otherwise `pro`.
- `auth check` and `auth list` show declared and observed values side by side.

## Commands

`<ref>` is a Figma URL or a bare file key. A `node-id` in the URL becomes the default `--node`. Every command that takes `<ref>` accepts `--node <id>` (repeatable, comma-separated) in either `1:2` or `1-2` form, including instance chains like `I1:2;3:4`.

```text
agent-figma auth add <profile> --form [--kind pat|plan] [--expires YYYY-MM-DD] [--seat ...] [--plan ...] [--default]
agent-figma auth add <profile> --token <token> ...        (or token on stdin)
agent-figma auth update <profile> [--form|--token ...] [--expires ...] [--seat ...] [--plan ...] [--default]
agent-figma auth check [profile] [--file <ref>]
agent-figma auth list | default <profile> | remove <profile>
agent-figma config list | get | set | unset               (timeout_ms, max_retries, max_wait_s, prefetch, cache_fresh_s)

agent-figma resolve <url>                                 no API call
agent-figma file meta <ref>                               Tier 3
agent-figma file outline <ref> [--node id] [--depth N]    structure only, cache first
agent-figma file index <ref>                              catalogue: pages, top frames, components/sets/variants, styles
agent-figma nodes get <ref> [--node id...] [--depth N] [--view all|layout|text|visuals|structure] [--raw] [--max-nodes N]
agent-figma nodes find <ref> [--name glob] [--type T] [--page name]     over the cache
agent-figma render <ref> --node id... [--as png|jpg|svg|pdf] [--scale N] [--out dir] [--url-only] [--with-related]
agent-figma assets fills <ref> [--node id] [--out dir]
agent-figma assets export <ref> --node id... [--out dir]  nodes with designer export settings
agent-figma components list <ref>  ·  components get <key>...
agent-figma component-sets list <ref>  ·  component-sets get <key>...
agent-figma styles list <ref>  ·  styles get <key>...
agent-figma variables list <ref> [--published]           Enterprise only
agent-figma comments list <ref>
agent-figma versions list <ref> [--limit N] [--cursor C]
agent-figma dev-resources list <ref> [--node id]
agent-figma cache status [<ref>] | path | clear [<ref>]
agent-figma api get <path>                               GET-only escape hatch
agent-figma usage
agent-figma mcp
```

The image type flag is `--as`, not `--format`: `--format/-f` is the family's persistent output-format flag.

Global flags: `--profile/-p`, `--token` (hidden from MCP, never echoed), `--format json|yaml|jsonl` (NDJSON when omitted), `--timeout`, `--debug`, `--max-retries`, `--max-wait`, `--refresh` (revalidate the cache now), `--offline` (never call Figma; answer from cache or fail), `--no-prefetch`. `--base-url` (hidden) and `AGENT_FIGMA_BASE_URL` point at the mock server.

Key routing: reads that accept a branch key (file, nodes, images, image fills, meta, versions, comments) get the branch key when the link has one; library endpoints (`components`, `component_sets`, `styles`) and `dev_resources` require the main file key, so they get `file_key` from the link. A bare key is used as given everywhere.

`resolve` parses `/file/`, `/design/`, `/proto/`, `/board/`, `/slides/`, `/deck/` and future `/<type>/<key>` paths on `figma.com` and subdomains; a `/branch/<key>` segment wins over the main key (and is reported alongside it). Non-design editor types are passed through; if Figma returns something the simplifier does not understand, the error says so.

## Output contract

The family contract from `lib-agent-output`: NDJSON on stdout, `@`-prefixed metadata lines, `{error, fixable_by, hint}` JSON on stderr, non-zero exit on failure. `--format json` wraps the same records in one envelope.

Metadata lines used across commands:

- `@file` — `{key, branch_key?, name, version, last_modified, editor_type, stale?, latest_version?}` for the file a command read. `stale: true` means the answer came from an older cached version (see Freshness).
- `@styles` — the shared style table for simplified nodes (below).
- `@related` — one per related item: `{rel, id, name?, type?, props?, render?, cost}`. `rel` is `variant|component_set|main_component|ancestor|page|render`. `cost` tells the agent what fetching it would spend: `cached` (free), `download` (CDN only, no API budget), or `tier1`.
- `@pagination` — the family shape `{has_more, next_cursor}`. For node output, `next_cursor` is a comma-separated list of the node ids still to print: those `--max-nodes` (default 400) left out, and nodes whose children were never fetched because Figma refused the subtree as too large. Passing it back as `--node` continues forward; following it to the end walks the cached subtree with no further Tier 1 calls. For Figma's paginated lists (versions) it is Figma's cursor, passed back as `--cursor`.
- `@unresolved` — the `lib-agent-cli` `EntityGet` shape `{id, reason, fixable_by, hint, retry_after_seconds}`, in input position, exit 0; used for ids Figma returned as null and for `get <key>...` misses.
- `@budget` — `{calls: {tier1, tier2, tier3}, cache_hits, prefetched: {nodes, renders}}` at the end of every command that could touch Figma.

### Simplified nodes

`nodes get` (default `--view all`) emits one line per node in depth-first order, after `@file` and `@styles`:

```json
{"id":"1:2","parent":"0:1","depth":1,"type":"FRAME","name":"Card","size":[320,180],"layout":"layout_1a2b3c4d","fills":"#FFFFFF","radius":8,"children":3}
```

Rules, adapted from Framelink's simplifier:

- Drop invisible nodes. Drop auto-generated names (`Frame 12`, `Rectangle 3`, …) and TEXT layer names (the text itself is kept).
- Collapse a container whose children are all vector-like into one leaf marked `svg: true` (except auto-layout containers with fewer than 10 children). It keeps its Figma type, so a collapsed icon component is still a `COMPONENT`; `VECTOR` nodes are also marked `svg: true`.
- Layout as flex/grid vocabulary: `mode` (`row|column|grid|none`), `gap`, CSS-shorthand `padding`, `justify`, `align`, `wrap`, `sizing` (`fill|hug|fixed`). Children outside auto-layout flow get `pos: [x, y]` relative to the parent; raw absolute boxes are not emitted.
- Paints as CSS-ish strings (`#RRGGBB`, `rgba(…)`, `linear-gradient(…)`); image paints as `{image_ref, scale_mode}`.
- Text: `text` content plus a `text_style` reference (family, weight, size, line height, letter spacing, alignment).
- Components: instances carry `component` (`{id, name, set?}`) and `props`; variant components carry `props` parsed from the variant name.
- `export: true` marks nodes with designer export settings.
- Numbers rounded to 2 decimal places.
- A style value (layout, fills, strokes, effects, text style) used by 2+ emitted nodes, or bound to a named Figma style, moves into `@styles` under a content-hashed id (`fill_1a2b3c4d`); single-use values stay inline.

`--view` narrows what is emitted: `structure` (id, parent, depth, type, name, size, children — what `file outline` prints), `layout`, `text`, `visuals`. `--raw` emits Figma's node JSON unchanged, one line per requested node.

### Files on disk

Downloads always go to the cache first and are named for humans:

```text
<cache>/<profile>/<file_key>/v-<version>/renders/<page-slug>/<node-slug>--<node-id>[@2x].<ext>
<cache>/<profile>/<file_key>/v-<version>/fills/<image_ref>.<ext>
```

`<cache>` is `xdg.CacheDir("agent-figma")`; profile names are sanitized before use in paths. A node not in the node store (so its page and name are unknown) is saved as `renders/<node-id>[@2x].<ext>` rather than spending a Tier 1 call to name it. `--out <dir>` copies files into `<dir>/<page-slug>/…`. Every downloaded file gets one manifest line: `{id, name, page, path, file, format, scale, bytes, source: render|fill|export, downloaded}`. `path` is absolute; `file` is a `lib-agent-output` FileRef (`{"@type": "file", root: "cache", path, ...}`) so an MCP client can read the image through the `fs` tool. Under MCP, `path` is omitted and only the FileRef is given. `downloaded: false` means the file was already on disk.

## Cache

Directories come from `lib-agent-cli/xdg` (cache and state dirs). The cache is scoped per profile so one account never reads another's data.

Per file version (`<cache>/<profile>/<file_key>/v-<version>/`):

- `nodes.json.gz` — a node store: every node Figma has returned for this version, keyed by id: its own properties, `parent`, `page`, and `known_depth`, the number of levels below it whose children lists are complete (`-1` = whole subtree, `0` = own properties only). `children` ids are stored only when `known_depth != 0`. Every Tier 1 response is decomposed into it and merged (taking the larger `known_depth`), so a fetch of a frame also caches all its descendants. Ancestors and pages that `GET /v1/files/:key?ids=` returns come with partial `children` arrays and are stored with `known_depth: 0`, so they are never mistaken for complete. The response's `components`/`componentSets` maps (node id → `{key, name, componentSetId, remote}`) are merged into the store too.
- `renders.json` — `(node_id, format, scale, options) → {url, fetched_at}` or a recorded null. Render URLs expire after 30 days; treat them as stale after 29.
- `fills.json` — `image_ref → {url, fetched_at}` from the one Tier 2 call; stale after 13 days.
- `library.json` — the Tier 3 components, component sets, and styles lists.
- The node store also records ids Figma said this version does not have (`missing`), so asking about a deleted or mistyped node again costs nothing; the mark clears if the node later arrives.
- downloaded renders and fills.

Per file (`<cache>/<profile>/<file_key>/head.json`): the latest known `version`, `checked_at`, and name. Content is always filed under the `version` the Tier 1 response reports, not the one a freshness check predicted, so an edit between the two cannot mix versions. Only the newest 3 version directories per file are kept. Writes to cache files go through `creds.Store`-style locked read-modify-write so parallel processes merge rather than clobber.

**Freshness.** A command first decides which version is current. If `head.json` was checked within `cache_fresh_s` (default 300), use its version. Otherwise call `GET /v1/files/:key/meta` (Tier 3; the response is `{file: {...}}` and `version` is optional there) and compare `version`.

A file being edited changes version constantly, and on a `low` seat refetching after every edit would burn the monthly Tier 1 allowance. So when the current version has nothing cached but an older version can answer, `serve_stale` decides: `on` answers from the older version with `@file.stale: true` and `latest_version`; `off` refetches; `auto` (default) is `on` for `low` seats and `off` otherwise. `--refresh` forces the check and a refetch, and is never answered from an older version; `--offline` never calls Figma and uses the newest cached version for the selected profile and the key the ref resolves to. If `/meta` fails (403 for a missing `file_metadata:read`, or no `version`), the command treats the cache as unvalidated, fetches, and emits a notice suggesting the scope.

A query is answered from the node store when the requested subtree is present to the requested depth; otherwise it fetches. `cache status` reports what is cached per file and version; `cache clear` removes it.

## Fetch wide, return lean (prefetch)

Because cost is per request, each Tier 1 call should carry the ids an agent is likely to want next. Three layers stay separate: what is **requested** from Figma (wide), what is **cached** (everything returned), and what is **printed** (what was asked, plus `@related` references).

Node fetches:

- With no `--depth`, fetch with `GET /v1/files/:key?ids=…` rather than `/nodes`: same tier, but it also returns the ancestor chain, all pages, and the local components the subtree depends on. `depth` on that endpoint counts from the document root, so when a depth is requested use `/v1/files/:key/nodes?ids=…&depth=N` (relative to each node). If Figma answers `400 Request too large`, fall back to `/nodes` with depth 4, then 2, then 1, note the cut on stderr, and list the depth-limited nodes in `@pagination.next_cursor`.
- Add, in the same call, ids the cache already knows are related and not yet cached: the component set of a requested variant (its subtree holds every sibling variant), and the local main component and set behind an instance. Which set a variant belongs to comes from the node store when the variant's parent is known, or from the cached Tier 3 library (`containing_frame.containingComponentSet`), which knows it before any Tier 1 call; `file index` or `components list` populates it. On a cold first touch of an unpublished variant nothing is known, so the widening pays off from the next call. At most 10 extra ids, since each extra id brings its whole subtree.
- When the fallback cut a subtree, the command emits a notice naming the ids and the records carry `truncated: true`.

Renders:

- Add sibling variants of a requested variant (and the variants of a requested component set) to the same `/v1/images` call, at the requested format and scale.
- Store every returned URL; download only the requested ids (`--with-related` downloads the rest).
- Widening uses only what the cache knows. A render never spends an extra Tier 1 call on metadata; an uncached node is named by id.

Image fills: the Tier 2 call always returns the whole file's map; cache all of it.

Related renders are planned from the cache whether or not the requested render needs a call: uncached ones ride along on a call that is happening anyway, cached ones are listed as `cost: download`, and `--with-related` downloads them, spending a render call of its own only when their URLs are not cached. A failed download forgets that URL under the cache lock, so the next run renders afresh.

Library: `file index` makes the Tier 3 calls (meta, components, component sets, styles) and, if the node store has no structure for the file, one Tier 1 call at the depth a default `file outline` uses (3 from the root), so the two together cost one Tier 1 call. Published components and their thumbnails let an agent see variants without any render. Unpublished components are only known once the node store has them.

Guardrails:

- Extras are best effort. If a widened call fails (render timeout, 500, `Request too large`), retry once with only the requested ids. Never bisect extras.
- Caps: at most 10 extra ids per node call and 24 per render call. Extras whose cached bounding box × scale² exceeds Figma's 32 MP render cap are skipped.
- `config set prefetch off|related` (default `related`). `--no-prefetch` turns it off for one command.
- Tier 2 reads (comments, versions, variables) are never speculative.

## Rate limiting

`internal/ratelimit` owns budget state in `<state>/agent-figma/ratelimit.json`, written under the `creds.Store` lock so parallel processes share it:

- Each request is classified to a tier by path.
- **Buckets.** Figma's buckets are per user × plan of the file × tier, so state is keyed by `(profile, plan bucket, tier)`, where the plan bucket is the plan Figma last reported for the file (`unknown` until a 429 says). A 429 on a Starter file then does not stall reads of a Pro file.
- **Cooldowns.** After a 429, record `cooldown_until = now + Retry-After` for the bucket, plus the reported plan tier and rate-limit type. Before sending, a request in a cooling-down tier waits if the remaining wait is ≤ `--max-wait` (default 60s), otherwise fails fast without calling Figma.
- **Pacing.** A sliding 60-second window of request timestamps per bucket; when the window is full for the effective budget (seat class × plan, defaults `high` × `pro`), the next request waits for room (same `--max-wait` rule). The lock is held only to read and update state, never while sleeping. Tier 1 on `low` seats and on Starter files is not paced per minute (the budget is monthly); instead every such call emits a notice with a persisted count of Tier 1 calls this calendar month (UTC).
- **Retries.** 429s honour `Retry-After` exactly and do not consume `--max-retries`, but the total time spent waiting on 429s within one request is capped at `--max-wait`; a wait above it returns immediately. 5xx (except render failures), 408, and transport errors retry with jittered exponential backoff up to `--max-retries` (default 2).
- **The error.** A 429 that is not waited out returns `fixable_by: retry` (or `human` when `rate_limit_type` is `low`, since only a seat change helps) with `retry_after_seconds` and a hint carrying the plan tier, rate-limit type, upgrade link, and whether an older cached version can be read with `--offline`.

Uncached reads (variables, comments, versions, dev resources) go through the same session pipeline for key routing and end with `@budget`. Variable records use snake_case keys, mode names rather than mode ids, CSS colours, and `{alias: <variable id>}` for aliases, matching node records.

## Errors worth special handling

- `400 Request too large` → fall back as above; if still too large, `fixable_by: agent` with a hint to request a narrower `--node`.
- `400`/`500` render timeout → retry without extras, then split the requested ids in two once.
- A `null` render URL → a per-id `@unresolved` line (node missing, invisible, or empty) on stdout, not a command failure.
- A `null` node in `/nodes`, or an id missing from a file response → per-id `@unresolved` with hints (wrong branch key, prototype or FigJam link, deleted node).
- Image fills respond `{error, status, meta: {images: {ref: url}}}`, unlike `/v1/images`' `{err, images}`.
- Export settings are `{suffix, format: JPG|PNG|SVG|PDF, constraint: {type: SCALE|WIDTH|HEIGHT, value}}`. `assets export` converts WIDTH/HEIGHT to a scale from the node's bounding box (clamped to 0.01–4), groups by `(format, scale)`, and spends one render call per group.
- 403 → distinguish expired/invalid token (`human`, `auth update --form`), missing scope (`human`, names the scope), and plan-limited endpoints such as variables (`human`, "Enterprise only").

## Package layout

```text
cmd/agent-figma        main
cmd/mockfigma          local mock server
internal/figmaurl      URL/key/node-id parsing (no network)
internal/config        config.json: profiles, defaults
internal/credential    keychain-backed token store
internal/ratelimit     tiers, cooldowns, pacing, observations
internal/api           HTTP client: auth headers, retries, error mapping, debug
internal/figma         typed response shapes (the subset we read)
internal/cache         per-profile, per-version node store, render/fill maps, blobs
internal/simplify      node simplification and the style table
internal/prefetch      widening planner for node and render calls
internal/fetch         cache-first orchestration over api + cache + prefetch
internal/mockfigma     synthetic fixture file and routes, fault injection
internal/output        writer indirection and Figma-specific record shaping
internal/cli/...       cobra commands
skills/agent-figma     the LLM-facing skill (SKILL.md + references/)
```

## Testing

- Unit tests per package; the CLI e2e suite runs in-process (`cli.RunForTest`) against `internal/mockfigma` via `httptest`, with `XDG_*` dirs in a temp dir and `LIB_AGENT_NO_KEYCHAIN=1`.
- `mockfigma` serves one synthetic file (pages, auto-layout frames, text, a variant component set with instances, vector groups, image fills, hidden nodes, export settings), a fake CDN for render and fill URLs, and per-tier request counters. It can be told to fail: `POST /_mock/faults` with rules such as `429:tier1:retry=2:type=low:plan=pro`, `400-too-large:/v1/files`, `render-timeout`, `null-render:<id>`.
- Prefetch and cache tests assert Tier 1 call counts from the mock, not just output.
- Live tests (`go test -tags live`) read `FIGMA_TOKEN` and `AGENT_FIGMA_LIVE_URL`, skip when either is missing, and assert only shape, never content.
- `make apicheck` (later) validates emitted paths and query parameters against Figma's published OpenAPI spec, as `agent-stripe` does with Stripe's.

`nodes find` searches only the cache (fetching the depth-2 outline first if nothing is cached) and ends with `@coverage {searched, matches, incomplete}`, where `incomplete` lists frames, sections, sets, and pages whose contents are not cached, so a miss inside them is not proof of absence.

## MCP

`agent-figma mcp` comes from `lib-agent-mcp` over the cobra tree, with `auth`, `config`, `usage`, and `cache` left out of the tool surface, `--token` hidden, `WithOAuthKeyringService("app.paulie.agent-figma.mcp")`, and the cache directory registered with `WithFileRoots(xdg.Root("cache", …))` so manifest paths resolve through the read-only `fs` tool rather than exposing host paths. `--out` is refused when running under MCP. The MCP command sets `AGENT_FIGMA_VIA_MCP=1` for the tool subprocesses it runs, which is how commands know.

## Deliberately not in v1

Team-scoped library lists (`--team`, paginated), writes of any kind, webhooks, OAuth, folder/project browsing (needs approval for some OAuth apps; team ids cannot be listed via the API anyway), plugin or desktop-app bridges, element templates in the simplifier, and code generation.
