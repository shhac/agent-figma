# Rendering and assets

Every download lands in the cache first and prints one **manifest line** per
file. Read the image at its `path` (plain CLI) or through the MCP `fs` tool
(see [Under MCP](#under-mcp)).

## render

```bash
agent-figma render "https://www.figma.com/design/FAKEFILEKEY0000000000000/Example-File?node-id=1-11"
agent-figma render FAKEFILEKEY0000000000000 --node 2:30,2:17           # one Tier 1 call for both
agent-figma render FAKEFILEKEY0000000000000 --node 1:10 --as jpg --scale 1
agent-figma render FAKEFILEKEY0000000000000 --node 2:17 --as svg
```

`--node` (or the link's `node-id`) is required. One Tier 1 call covers every
uncached id in the command, so batch the nodes you need. A render already in
the cache (same node, format, scale, and options) spends nothing.

```json
{"@file":{"key":"FAKEFILEKEY0000000000000","name":"Example File","version":"1000000000000000001","last_modified":"2026-01-15T10:00:00Z","editor_type":"figma"}}
{"bytes":74,"downloaded":true,"file":{"@type":"file","root":"cache","path":"work/FAKEFILEKEY0000000000000/v-1000000000000000001/renders/components/state-default-size-md--1-11@2x.png","name":"state-default-size-md--1-11@2x.png","mimetype":"image/png"},"format":"png","id":"1:11","name":"State=Default, Size=md","page":"Components","path":"/home/example/.cache/agent-figma/work/FAKEFILEKEY0000000000000/v-1000000000000000001/renders/components/state-default-size-md--1-11@2x.png","scale":2,"source":"render"}
{"@related":{"rel":"render","id":"1:12","name":"State=Hover, Size=md","render":"png@2x","cost":"download"}}
{"@related":{"rel":"render","id":"1:13","name":"State=Disabled, Size=md","render":"png@2x","cost":"download"}}
{"@budget":{"cache_hits":0,"calls":{"tier1":1,"tier2":0,"tier3":0},"prefetched":{"nodes":0,"renders":2}}}
```

### Flags

| Flag | Meaning |
|---|---|
| `--as png\|jpg\|svg\|pdf` | image format (default `png`). **Not `--format`**: that is the output format of the JSON |
| `--scale N` | 0.01-4 for png/jpg (default 2). svg and pdf always render at 1, whatever `--scale` says |
| `--with-related` | also download the sibling variants rendered alongside (listed as `@related`, `cost: download`) |
| `--url-only` | print Figma's render URL (valid about 30 days) instead of downloading |
| `--out <dir>` | also copy the images into `<dir>/<page-slug>/`; refused under MCP |
| `--contents-only` | render only the node, not content overlapping it (default true) |
| `--absolute-bounds` | use the node's full bounds, ignoring cropping |
| `--svg-outline-text`, `--svg-include-id`, `--svg-simplify-stroke` | SVG options (outline text and simplify stroke default true) |

### Sibling variants come for free

Rendering a variant (or a component set) adds its sibling variants to the
same call, at the same format and scale. Their URLs are cached and listed as
`@related` with `cost: "download"`: rendering them later costs no API call
(`@budget.cache_hits: 1`, `tier1: 0`). Pass `--with-related` to download them
in the same command. Widening only uses what the node cache already knows,
so it works best after a `nodes get` or `file index` of the set; on a cold
cache only the requested ids are rendered. `--no-prefetch` turns it off.

### Names and paths

Files are named for humans when the node is cached:
`renders/<page-slug>/<node-slug>--<node-id>[@<scale>x].<ext>` (no suffix at scale 1). A node not in the
node cache is saved as `renders/<node-id>[@<scale>x].<ext>` with empty `name` and
`page`, rather than spending a Tier 1 call to name it.

### Nothing rendered

An invisible, empty, or missing node renders as nothing; that is a per-id
meta line, not a failure:

```json
{"@unresolved":{"id":"2:16","reason":"Figma rendered nothing for 2:16","fixable_by":"agent","hint":"The node may not exist, be invisible or 0% opacity, or have no renderable content; check it with 'nodes get'."}}
```

## Manifest fields

| Field | Meaning |
|---|---|
| `id`, `name`, `page` | the node (name/page empty if it was not cached) |
| `format`, `scale` | what was rendered (`render`, `export`) |
| `source` | `render`, `export`, or `fill` |
| `path` | absolute path to read. With `--out`, this is the copy in `--out` |
| `cache_path` | with `--out`, the original in the cache |
| `bytes` | file size |
| `downloaded` | `true` if fetched now, `false` if it was already on disk |
| `file` | FileRef `{"@type":"file", root:"cache", path, name, mimetype}`: the same file relative to the cache root, for the MCP `fs` tool |
| `url` | `--url-only` only, in place of `path`/`file` |
| `image_ref`, `mime`, `nodes`, `scale_mode` | `assets fills` only: the fill's ref, its type, the nodes using it (with `--node`), how it is laid out |

## Under MCP

An MCP client has no filesystem, so the server drops `path` and you read the
`file` reference with the built-in read-only `fs` tool, passing `file.path`
verbatim:

```text
fs get  cache work/FAKEFILEKEY0000000000000/v-1000000000000000001/renders/screens/card--2-30@2x.png
fs find cache -e png
```

`--out` is refused under MCP (`fixable_by: agent`).

## assets fills

Image fills (photos and bitmaps placed in shapes; nodes show them as
`"fills":{"image_ref":"fakeimageref0001","scale_mode":"fill"}`). Figma returns
the whole file's fill map in one Tier 2 call; it is cached.

```bash
agent-figma assets fills FAKEFILEKEY0000000000000                 # every fill in the file
agent-figma assets fills FAKEFILEKEY0000000000000 --node 2:10     # only fills used under 2:10
```

```json
{"bytes":74,"downloaded":true,"file":{"@type":"file","root":"cache","path":"work/FAKEFILEKEY0000000000000/v-1000000000000000001/fills/fakeimageref0001.png","name":"fakeimageref0001.png","mimetype":"image/png"},"image_ref":"fakeimageref0001","mime":"image/png","nodes":["2:13"],"path":"/home/example/.cache/agent-figma/work/FAKEFILEKEY0000000000000/v-1000000000000000001/fills/fakeimageref0001.png","scale_mode":"fill","source":"fill"}
```

`--node` reads those subtrees cache-first to find which fills they use (a
Tier 1 call if they are not cached). Without it, `nodes` and `scale_mode`
are empty. `--out <dir>` copies into `<dir>/fills/`.

## assets export

Renders what designers marked for export (nodes with `"export": true`), at
the settings they chose (format, scale or width/height constraint).

```bash
agent-figma assets export FAKEFILEKEY0000000000000 --node 2:10
```

```json
{"bytes":74,"downloaded":true,"file":{…},"format":"png","id":"2:17","name":"Export me","page":"Screens","path":"…/renders/screens/export-me--2-17@2x.png","scale":2,"source":"export"}
{"bytes":129,"downloaded":true,"file":{…},"format":"svg","id":"2:17","name":"Export me","page":"Screens","path":"…/renders/screens/export-me--2-17.svg","scale":1,"source":"export"}
```

`--node` is required (the subtrees to search). Width/height constraints are
converted to a scale from the node's size (clamped to 0.01-4). It spends one
Tier 1 render call per distinct format+scale, so two settings above cost two.
`--out <dir>` copies into `<dir>/<page-slug>/`.
