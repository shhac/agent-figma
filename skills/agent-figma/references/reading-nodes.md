# Reading nodes

`nodes get` and `file outline` answer from the node cache when the requested
subtree is already there to the requested depth (`@budget.cache_hits: 1`,
no calls). Otherwise they spend **one** Tier 1 call for everything asked for,
so batch: `--node 2:30,2:17,1:10` is one call; three commands may be three.

## nodes get

```bash
agent-figma nodes get "https://www.figma.com/design/FAKEFILEKEY0000000000000/Example-File?node-id=1-11"
agent-figma nodes get FAKEFILEKEY0000000000000 --node 2:10 --view text
agent-figma nodes get FAKEFILEKEY0000000000000 --node 2:10 --depth 1
```

Output order: `@file`, `@styles`, one record per node (depth-first),
`@unresolved`, `@related`, `@pagination`, `@budget`.

```json
{"@file":{"key":"FAKEFILEKEY0000000000000","name":"Example File","version":"1000000000000000001","last_modified":"2026-01-15T10:00:00Z","editor_type":"figma"}}
{"@styles":{"fill_972ecd24":{"name":"Brand/Primary","value":"#3B5BF5"},"text_75082aa8":{"name":"Body/Label","value":{"align":"center","font":"Inter","line_height":"20px","size":"14px","weight":600}}}}
{"id":"1:11","parent":"1:10","depth":0,"type":"COMPONENT","name":"State=Default, Size=md","size":[120,40],"layout":{"align":"center","gap":"8px","justify":"center","mode":"row","padding":"10px 16px"},"fills":"fill_972ecd24","radius":8,"props":{"Size":"md","State":"Default"},"children":1}
{"id":"1:14","parent":"1:11","depth":1,"type":"TEXT","size":[88,20],"layout":{"sizing":{"h":"hug","v":"hug"}},"fills":"#FFFFFF","text":"Button","text_style":"text_75082aa8"}
{"@related":{"rel":"component_set","id":"1:10","of":"1:11","name":"Button","type":"COMPONENT_SET","cost":"cached"}}
{"@related":{"rel":"variant","id":"1:12","of":"1:11","name":"State=Hover, Size=md","type":"COMPONENT","props":{"Size":"md","State":"Hover"},"cost":"cached"}}
{"@related":{"rel":"variant","id":"1:13","of":"1:11","name":"State=Disabled, Size=md","type":"COMPONENT","props":{"Size":"md","State":"Disabled"},"cost":"cached"}}
{"@related":{"rel":"page","id":"1:1","of":"1:11","name":"Components","type":"CANVAS","cost":"tier1"}}
{"@budget":{"cache_hits":0,"calls":{"tier1":1,"tier2":0,"tier3":0},"prefetched":{"nodes":1,"renders":0}}}
```

### Record fields

| Field | Meaning |
|---|---|
| `id`, `parent`, `depth`, `type`, `name` | `depth` is relative to the requested node (0). Auto-generated names (`Frame 12`) and TEXT layer names are dropped |
| `size` | `[w, h]` in px |
| `pos` | `[x, y]` relative to the parent, only for children outside auto-layout flow |
| `layout` | flex vocabulary: `mode` (`row\|column\|grid\|none`), `gap`, `padding` (CSS shorthand), `justify`, `align`, `wrap`, `sizing: {h, v}` (`fill\|hug\|fixed`), `position: "absolute"` |
| `fills` | `#RRGGBB`, `rgba(…)`, `linear-gradient(…)`, or `{image_ref, scale_mode}` for an image (download it with `assets fills`) |
| `strokes`, `effects`, `radius`, `opacity` | CSS-ish values |
| `text`, `text_style` | content (bold/italic runs as Markdown, e.g. `Total: **$42.00**`) and `{font, weight, size, line_height, letter_spacing, align}` |
| `component` | on an INSTANCE: `{id, name, set}` of its main component |
| `props` | instance property values, or a variant's props parsed from its name |
| `export: true` | the designer set export settings; `assets export` renders them |
| `svg: true` | a vector, or a container drawn only with vector shapes (an icon, logo, illustration): best exported with `render --as svg` |
| `children` | count of visible children |
| `truncated: true` | it has children that were not printed (depth limit, `--max-nodes`, or not fetched) |

Invisible nodes are dropped. A container (FRAME, GROUP, COMPONENT, INSTANCE,
BOOLEAN_OPERATION) whose visible children are all vector shapes keeps its
own `type` but is printed as one leaf with `"svg": true` and no children;
VECTOR nodes carry `svg: true` too. There is no `SVG` type: filter on the
`svg` flag. Auto-layout containers with fewer than 10 children are never
collapsed, since they are usually real layout.

```json
{"id":"1:20","parent":"1:1","depth":0,"type":"COMPONENT","name":"Icon/Star","size":[24,24],"svg":true}
{"id":"2:15","parent":"2:10","depth":0,"type":"GROUP","name":"Logo","size":[48,48],"svg":true}
```

A value used by two or more printed nodes, or bound to a named Figma style,
moves into `@styles` and the record holds its id (`"fills":"fill_972ecd24"`).
Named styles carry `name` (`Brand/Primary`): use that name rather than the
hex when describing the design.

### Views

`--view` narrows each record:

| View | Keeps |
|---|---|
| `all` (default) | everything above |
| `structure` | `id, parent, depth, type, name, size, children`, plus a one-line `text` preview (≤80 chars) on TEXT nodes (what `file outline` prints) |
| `layout` | structure + `layout`, `pos` |
| `text` | structure + `text`, `text_style`, and `fills` on TEXT nodes |
| `visuals` | structure + `fills`, `strokes`, `effects`, `radius`, `opacity` |

`component`, `props`, and `export` appear only in `all`.

`--depth N` limits levels below each requested node (`-1`, the default, is
the whole subtree). `--raw` prints Figma's own node JSON, one line per
requested node, unchanged (camelCase, absolute boxes); it is cache-first too.
Use it only when the simplified record lacks a field you need.

### Unresolved ids

An id Figma does not have comes back as a meta line, exit 0:

```json
{"@unresolved":{"id":"99:99","reason":"Figma has no node 99:99 in this file","fixable_by":"agent","hint":"Check the id with 'agent-figma file outline <ref>'; prototype and FigJam links can use different ids than the design file."}}
```

Misses are remembered per file version: asking for the same id again
answers `@unresolved` from the cache with no Tier 1 call. A new file version
forgets them, so an id a designer has just added is looked up afresh.

### @related

Nodes worth reading next, so you do not have to explore: `rel` is
`ancestor`, `page`, `component_set`, `variant` (siblings in the set),
`main_component` (of an instance); `render` lines come from `render`. `of` is the printed node it
relates to. Prefer entries with `cost: "cached"`: reading them is free.

### Paging large subtrees (--max-nodes)

Output stops at `--max-nodes` records (default 400), chosen breadth-first so
the top of the tree survives. Records with children left out carry
`truncated: true`, and `@pagination.next_cursor` lists the node ids still to
print:

```bash
agent-figma nodes get FAKEFILEKEY0000000000000 --node 2:10 --max-nodes 4 --view structure
```

```json
{"id":"2:10","parent":"2:1","depth":0,"type":"FRAME","name":"Checkout","size":[375,940],"children":8,"truncated":true}
{"id":"2:11","parent":"2:10","depth":1,"type":"INSTANCE","name":"Button","size":[120,40],"children":1,"truncated":true}
{"id":"2:12","parent":"2:10","depth":1,"type":"TEXT","size":[327,24]}
{"id":"2:13","parent":"2:10","depth":1,"type":"RECTANGLE","name":"Hero image","size":[327,180]}
{"@pagination":{"has_more":true,"next_cursor":"2:14,2:15,2:17,2:18,2:30,I2:11;1:14"}}
```

To continue, pass `next_cursor` back as `--node`, **quoted** (instance ids
contain `;`), with the same flags:

```bash
agent-figma nodes get FAKEFILEKEY0000000000000 --node '2:14,2:15,2:17,2:18,2:30,I2:11;1:14' --max-nodes 4 --view structure
```

Each page prints the next records (depth restarts at 0 for each listed
node) and a new cursor, until `@pagination` stops appearing. Nothing is
printed twice. The first call cached the whole subtree, so every
continuation is a cache hit: following the cursor to the end spends no
further Tier 1 calls. Raising `--max-nodes`, narrowing with `--view` or
`--depth`, or asking for one subtree by id (`--node 2:30`) are cheaper on
tokens when you do not need everything.

### Subtrees Figma refuses as too large

When Figma refuses a whole subtree as too large, the CLI retries the
`/nodes` endpoint at depth 4, then 2, then 1, keeps the first that
succeeds, and says so on stderr:

```json
{"notice":"Figma refused the full subtree of 2:10 as too large; showing a depth-limited tree","hint":"Records marked truncated have more children; request them with 'nodes get --node <id>'."}
```

Each attempt is a Tier 1 call, so this can spend up to four. Nodes whose
children were never fetched are marked `truncated` and listed in
`next_cursor`; passing it back as `--node` fetches those smaller subtrees
(one Tier 1 call for the batch). The error `Figma refused the request as too
large` appears only when even depth 1 is refused.

## file outline

Structure only, cache first. Without `--node`: every page and the frames on
it, `--depth` (default 2) levels below each page. With `--node`: that subtree.

```bash
agent-figma file outline FAKEFILEKEY0000000000000
agent-figma file outline FAKEFILEKEY0000000000000 --node 2:30 --depth 3
```

```json
{"id":"2:1","parent":"0:0","depth":0,"type":"CANVAS","name":"Screens","children":1}
{"id":"2:10","parent":"2:1","depth":1,"type":"FRAME","name":"Checkout","size":[375,940],"children":8}
{"id":"2:11","parent":"2:10","depth":2,"type":"INSTANCE","name":"Button","size":[120,40],"truncated":true}
```

`file index` caches the depth a default `file outline` prints, so
`file index` then `file outline` costs one Tier 1 call between them. A
deeper `--depth` or a `--node` subtree not yet cached spends its own call.
`--max-nodes` pages the outline the same way as `nodes get`: pass
`next_cursor` back as `--node`.

## nodes find

Searches every node cached for the file's current version: no Tier 1 call
unless nothing at all is cached (then it fetches the page/frame outline once).

```bash
agent-figma nodes find FAKEFILEKEY0000000000000 --name 'button*'
agent-figma nodes find FAKEFILEKEY0000000000000 --type TEXT --page Screens
```

```json
{"id":"1:10","name":"Button","page":"Components","parent":"1:1","size":[424,72],"type":"COMPONENT_SET"}
{"id":"2:11","name":"Button","page":"Screens","parent":"2:10","size":[120,40],"type":"INSTANCE"}
{"id":"2:18","name":"Button","page":"Screens","parent":"2:10","size":[120,40],"type":"INSTANCE"}
{"@coverage":{"incomplete":[],"matches":3,"searched":36}}
```

With less cached, `incomplete` names the frames still to deepen:

```json
{"@coverage":{"incomplete":["0:2","1:10","2:10"],"matches":1,"searched":8}}
```

`--name` is a case-insensitive glob or substring matched against layer
names (TEXT layers included, though `nodes get` hides their names); `--type` is a Figma node type (`FRAME`, `COMPONENT`, `COMPONENT_SET`,
`INSTANCE`, `TEXT`, …); `--limit` defaults to 100.

`@coverage` says how much was searched. `incomplete` (`[]` when the
search covered everything) lists frames whose
contents are **not** cached, so a miss there is not proof the node does not
exist: deepen those with `nodes get --node <id>` (batch them) and search
again.
