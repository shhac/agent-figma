# agent-figma

Figma CLI for AI agents: read file and node metadata, render node images, and download image assets with compact, LLM-shaped output and no direct access to Figma tokens.

## Why

Agents are increasingly asked to "look at this Figma link" — to build a screen, check spacing, or pull an icon. Figma's REST API makes that expensive in two ways: raw file JSON is enormous and full of noise an LLM has to wade through, and the endpoints that return it (files, nodes, image renders) sit in a rate-limit tier that allows 10-20 requests a minute on a Full seat and only a handful **a month** on a Collab or View seat. `agent-figma` answers from a per-version cache whenever it can, makes each scarce call carry what you are likely to ask for next, and prints simplified, flex-style node records that say what a design *is* rather than how Figma stores it. It belongs to the `agent-*` CLI family (`agent-slack`, `agent-stripe`, …) and shares their output contract and credential handling.

## Features

- **Link-first**: pass any figma.com link (`/design/`, `/file/`, `/proto/`, `/board/`, branch links) or a bare file key; `node-id` in the link becomes the target node. `resolve` parses links with no API call.
- **LLM-shaped nodes**: one NDJSON record per node with layout as flex vocabulary (`mode`, `gap`, `padding`, `sizing`), CSS-ish paints, text with its style, instance props and variant props; auto-generated names, hidden layers, and vector noise are dropped (icons and illustrations become one leaf marked `"svg": true`, keeping their Figma type); repeated values move into a shared `@styles` table. `--view structure|layout|text|visuals` narrows further; `--raw` gives Figma's JSON.
- **Budget-aware by design**: a per-profile, per-version node cache that every Tier 1 response is merged into; batching; prefetch of sibling variants into the same call; shared cooldowns across parallel processes; stale answers for low seats when a file is mid-edit; unknown node ids remembered per version; `--offline` and `--refresh`. Every file read ends with an `@budget` line saying what it spent.
- **Related things up front**: `@related` lines list ancestors, the page, the component set, sibling variants, and main components, each with what reading it would cost (`cached`, `download`, or `tier1`).
- **Renders and assets on disk**: `render` (png/jpg/svg/pdf), `assets fills` (image fills), and `assets export` (designer export settings) save human-named files and print a manifest line per file.
- **Library and file reads**: `file index` catalogue (pages, frames, component sets with variants and thumbnails, styles), components, component sets, styles, variables (Enterprise; values per mode name, colours as CSS, aliases as `{"alias": id}`), comments, versions, dev resources, plus a GET-only `api get` escape hatch.
- **Structured errors**: JSON on stderr with `fixable_by` (`agent` | `human` | `retry`), a `hint`, and `retry_after_seconds` for rate limits — including whether a seat change, not waiting, is what would help.
- **Keychain-first credentials**: tokens are entered through a native dialog (`--form`), stored in the OS keychain, and never printed, logged, or echoed in errors.
- **MCP server**: `agent-figma mcp` exposes the read commands as MCP tools, with a read-only `fs` tool for the downloaded images.

Read-only: nothing writes to Figma.

## Install

```bash
go install github.com/shhac/agent-figma/cmd/agent-figma@latest
```

Or build from source:

```bash
git clone https://github.com/shhac/agent-figma.git
cd agent-figma
make build
./agent-figma usage
```

The LLM-facing skill lives in [`skills/agent-figma/`](skills/agent-figma/SKILL.md).

## Quick start

A human creates a personal access token in Figma (Settings > Security > Personal access tokens, read-only scopes — `agent-figma usage` lists them), then:

```bash
agent-figma auth add work --form --expires 2026-12-31 --seat dev --default
agent-figma auth check
```

`--seat collab|view` tells the CLI to treat Tier 1 as a monthly allowance from the first call. When guiding a user through setup, always use `--form`; never paste tokens into chat.

## Examples

All identifiers below are synthetic.

```bash
# Parse a link (no API call)
agent-figma resolve "https://www.figma.com/design/FAKEFILEKEY0000000000000/Example-File?node-id=2-10"

# Catalogue a file: pages, top frames, component sets with variants, styles
agent-figma file index FAKEFILEKEY0000000000000

# Structure only
agent-figma file outline FAKEFILEKEY0000000000000 --depth 1

# Read the linked node, simplified
agent-figma nodes get "https://www.figma.com/design/FAKEFILEKEY0000000000000/Example-File?node-id=2-30"
agent-figma nodes get FAKEFILEKEY0000000000000 --node 2:10 --view text

# Search what is cached (no Tier 1 call)
agent-figma nodes find FAKEFILEKEY0000000000000 --name 'button*'

# Render and download (into the cache; --out copies elsewhere)
agent-figma render FAKEFILEKEY0000000000000 --node 1:11 --with-related
agent-figma render FAKEFILEKEY0000000000000 --node 2:17 --as svg --out ./exports

# Image fills and designer exports
agent-figma assets fills FAKEFILEKEY0000000000000 --node 2:10
agent-figma assets export FAKEFILEKEY0000000000000 --node 2:10

# Library, comments, history
agent-figma components list FAKEFILEKEY0000000000000
agent-figma comments list FAKEFILEKEY0000000000000
agent-figma versions list FAKEFILEKEY0000000000000 --limit 5

# Budget and cache
agent-figma cache status
agent-figma nodes get FAKEFILEKEY0000000000000 --node 2:30 --offline
```

A node record:

```json
{"id":"2:30","parent":"2:10","depth":0,"type":"FRAME","name":"Card","size":[327,120],"layout":{"align":"center","gap":"12px","mode":"row","padding":"16px"},"fills":"#FFFFFF","strokes":{"paint":"#E2E4E8","weight":"1px"},"effects":"effect_e3838271","radius":12,"children":1}
```

The trailing budget line:

```json
{"@budget":{"cache_hits":1,"calls":{"tier1":0,"tier2":0,"tier3":0},"prefetched":{"nodes":0,"renders":0}}}
```

## Output

NDJSON on stdout: records, then `@`-prefixed meta lines (`@file`, `@styles`, `@unresolved`, `@related`, `@pagination`, `@coverage`, `@budget`). Empty fields are left out. Large subtrees page with `--max-nodes`: pass `@pagination.next_cursor` back as `--node` to continue from the cache. `--format json|yaml` wraps the same output in one object. Errors are a single JSON object on stderr with a non-zero exit:

```json
{"error":"Figma rejected the token: Invalid token","fixable_by":"human","hint":"The token is invalid or expired. Generate a new one in Figma (Settings > Security > Personal access tokens) and run 'agent-figma auth update <profile> --form'."}
```

Profile metadata is in `$XDG_CONFIG_HOME/agent-figma/config.json`, the cache in `$XDG_CACHE_HOME/agent-figma/`, and rate-limit state (shared between processes) in `$XDG_STATE_HOME/agent-figma/`.

## MCP

```bash
agent-figma mcp                 # stdio, launched by an MCP client
agent-figma mcp --http :8000    # Streamable HTTP (bind to loopback or add --oauth local)
agent-figma mcp usage           # registration snippets, OAuth, and pairing
```

For Claude Code: `claude mcp add agent-figma -- agent-figma mcp`.

The read commands become tools (`auth`, `config`, `cache`, and `usage` are left out), `--token` is hidden, and `--out` is refused. Downloaded files come back as `{"@type":"file","root":"cache","path":…}` references that the built-in read-only `fs` tool serves (`fs get cache <path>`), so no host paths are exposed.

## Development

```bash
make build       # ./agent-figma
make test        # go test ./... -count=1
make vet
make lint        # golangci-lint v2
make fmt
make dev ARGS="usage"
```

`mockfigma` is a local stand-in for the Figma API with one synthetic file, a fake CDN, per-tier request counters, and fault injection (429s, too-large, render timeouts, plan limits). See [`design-docs/mock-figma.md`](design-docs/mock-figma.md).

```bash
make mock                                  # serve it on 127.0.0.1:12112
make mock-dev ARGS="file index FAKEFILEKEY0000000000000"   # run the CLI against it
curl -s localhost:12112/_mock/stats        # what the CLI spent, by tier
curl -s -XPOST 'localhost:12112/_mock/faults?rules=429:tier1:retry=600:type=low'
```

Live tests against the real API are opt-in: set `FIGMA_TOKEN` and `AGENT_FIGMA_LIVE_URL` (a design URL the token can read). Design decisions are in [`design-docs/`](design-docs/).

## License

[PolyForm Perimeter License 1.0.0](LICENSE) — © 2026 Paul Somers.
