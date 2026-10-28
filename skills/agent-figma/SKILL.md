---
name: agent-figma
description: |
  Read Figma design files cheaply from the terminal: resolve figma.com links
  to file keys and node ids, outline pages and frames, read nodes as compact
  layout/text/visual records, render nodes to png/jpg/svg/pdf, download image
  fills and designer exports, and read components, component sets, styles,
  variables, comments, versions, and dev resources. Use when asked to look at
  a Figma link or frame, inspect a design's layout, spacing, colours, or
  typography, export or screenshot a node, list a file's components or
  variants, or read comments on a design. Triggers: figma.com/design,
  node-id, Figma 429, rate limit exceeded.
allowed-tools: Bash(agent-figma *) Read
---

# agent-figma

Read-only Figma CLI. NDJSON on stdout (records, then `@`-prefixed meta lines);
`--format json|yaml` wraps the same output in one object (`data` plus the
`@` keys). Errors are one JSON object on stderr with a non-zero exit:
`{"error","fixable_by":"agent|human|retry","hint"?,"retry_after_seconds"?}`.
`agent` → fix the input and re-run; `human` → a person must act (token,
seat, plan, access); `retry` → wait `retry_after_seconds`, then re-run.
Non-fatal `{"notice","hint"}` lines on stderr are not failures.

`<ref>` is any figma.com link (`/design/`, `/file/`, `/proto/`, `/board/`,
`/branch/` …) or a bare file key. The link's `node-id` becomes `--node`.
Node ids take `1:2` or `1-2`, and instance chains like `I2:11;1:14`. Quote
links: `&` in a URL breaks the shell.

## Setup (once)

A human creates a personal access token in Figma (Settings > Security >
Personal access tokens, read-only scopes) and runs
`agent-figma auth add <profile> --form --expires YYYY-MM-DD [--seat full|dev|collab|view]`,
which opens a native dialog so the token never passes through chat. Never ask
for a token in the conversation or pass one on the command line yourself.
`agent-figma auth check` confirms who you are and what budget applies. Detail:
[references/setup-and-auth.md](references/setup-and-auth.md).

## Output contract

| Line | Meaning |
|---|---|
| `@file` | `{key, name, version, last_modified, editor_type}`; `stale: true` + `latest_version` when answered from an older cached version |
| `@styles` | shared style table: records reference ids like `fill_972ecd24`, `text_75082aa8` |
| `@components` | design-system components and variants used, with counts and node ids (instances are folded leaves; `--expand-instances` to see inside) |
| records | one per node, depth-first (`id, parent, depth, type, name, size, layout, fills, text, svg?, …`), or one per downloaded file (a manifest line). Empty fields are left out |
| `@unresolved` | `{id, reason, fixable_by, hint}` for an id Figma does not have or rendered as nothing; exit stays 0, and the miss is remembered for that file version |
| `@related` | `{rel, id, of, name, type, props?, render?, cost}`: nearby nodes you may want next |
| `@pagination` | `{has_more, next_cursor}`: more records were cut. For `nodes get` / `file outline`, pass `next_cursor` back as `--node` (quoted) to continue; for `versions list`, as `--cursor` |
| `@coverage` | `nodes find` only: `{matches, searched, incomplete}` |
| `@budget` | `{calls: {tier1, tier2, tier3}, cache_hits, prefetched}`: what this command spent; every read of a file `<ref>` ends with it (`resolve` and library `get <key>` do not) |

`cost` on `@related` says what fetching it spends: `cached` (free, already
stored), `download` (render URL already known, CDN only, no API budget), or
`tier1` (scarce).

## Rate limits: the one thing to remember

Figma meters **requests** per tier. **Tier 1** (file, nodes, renders) is
scarce: 10-20 a minute on Full/Dev seats, and only a handful **a month** on
Collab/View seats or Starter-plan files. Tier 2 (fills, comments, versions,
variables) is moderate; Tier 3 (meta, components, styles) is cheap.

- Everything is cached per profile and file version; repeat reads are free.
- Prefer `@related` entries with `cost: cached` or `download` over anything
  costing `tier1`. Ask for several nodes in **one** call (`--node a,b,c`)
  rather than one call each.
- Use `file index` (mostly Tier 3; published components carry
  `thumbnail_url`) before rendering variants one by one.
- `--offline` answers from the cache only; use it when budget is exhausted.
  `--refresh` always fetches the current version (a Tier 1 call if it is not
  cached yet); use it only when the user needs the latest edits.
- Short 429s are waited out (`--max-wait`, 60s). A `fixable_by: human` 429
  means a low seat's monthly allowance; stop spending Tier 1 and tell the user.

Detail: [references/rate-limits-and-cache.md](references/rate-limits-and-cache.md).

## Recommended workflow

```bash
agent-figma resolve "https://www.figma.com/design/FAKEFILEKEY0000000000000/Example-File?node-id=2-10"  # no API call
agent-figma file index FAKEFILEKEY0000000000000          # pages, top frames, components + variants, styles
agent-figma file outline FAKEFILEKEY0000000000000        # structure only, 2 levels; free right after file index
agent-figma nodes get "<link>"                           # the linked node, simplified, with @related
agent-figma nodes get FAKEFILEKEY0000000000000 --node 2:30 --view layout   # narrow to what you need
agent-figma nodes find FAKEFILEKEY0000000000000 --name 'button*'           # search the cache, no Tier 1
agent-figma render FAKEFILEKEY0000000000000 --node 2:30  # png @2x into the cache; Read the manifest path
agent-figma assets fills FAKEFILEKEY0000000000000 --node 2:10   # image fills used under a frame
```

Read the node record before rendering: layout, colour, and text questions are
answered by `nodes get` without spending a render. Render when you need to
see it; records with `"svg": true` are icons and illustrations best rendered
`--as svg`. Render manifests carry an absolute `path` you can `Read`; under MCP
(`agent-figma mcp`) read the `file` reference with the `fs` tool instead
(`fs get cache <file.path>`).

## The three common jobs

| Task | Command |
|---|---|
| Build a screen or component | `agent-figma handoff <ref> --node <id> --out ./design/<name>` — one folder: screenshot, tree, components, styles, icons, images |
| "The designs changed — what's different?" | `agent-figma diff <ref> --node <id>` — per-field changes since the last cached version (`--render` for before/after) |
| "Does my implementation match?" | screenshot the component, then `agent-figma compare <ref> --node <id> --image shot.png` — match %, differing regions named by design node, diff image |

Details and examples: [references/building-and-verifying.md](references/building-and-verifying.md).

## Router

| Need | Read |
|---|---|
| Every command and flag at a glance | [references/commands.md](references/commands.md) |
| Profiles, tokens, scopes, seat/plan declarations, expiry | [references/setup-and-auth.md](references/setup-and-auth.md) |
| Node record fields, `svg`, `--view`, `--raw`, `--max-nodes` paging, too-large subtrees, `nodes find` + `@coverage` | [references/reading-nodes.md](references/reading-nodes.md) |
| `render` flags and scale rules, `--with-related`, `--url-only`, `--out`, `assets fills/export`, manifest fields | [references/rendering-and-assets.md](references/rendering-and-assets.md) |
| `file index`, components / component sets / styles, variables, comments, versions, dev resources, branch links | [references/library-and-other-reads.md](references/library-and-other-reads.md) |
| Tiers, seats and plans, cooldowns, stale answers, `--refresh` / `--offline`, cache, prefetch, config keys | [references/rate-limits-and-cache.md](references/rate-limits-and-cache.md) |
| `handoff`, `diff`, `compare`: building, tracking design changes, verifying an implementation | [references/building-and-verifying.md](references/building-and-verifying.md) |
| An error or notice and what to do about it | [references/errors.md](references/errors.md) |

`agent-figma usage` prints the built-in reference card; `agent-figma <cmd> --help`
lists exact flags.
