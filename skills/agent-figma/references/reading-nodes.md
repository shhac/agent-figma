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
| `children` | count of visible children |
| `truncated: true` | it has children that were not printed (depth limit, `--max-nodes`, or not cached) |

Invisible nodes are dropped. A container whose children are all vector-like
(and any VECTOR) is printed as one `type: "SVG"` leaf (icons, logos, illustrations); render it
with `--as svg` if you need the shape.

A value used by two or more printed nodes, or bound to a named Figma style,
moves into `@styles` and the record holds its id (`"fills":"fill_972ecd24"`).
Named styles carry `name` (`Brand/Primary`): use that name rather than the
hex when describing the design.

### Views

`--view` narrows each record:

| View | Keeps |
|---|---|
| `all` (default) | everything above |
| `structure` | `id, parent, depth, type, name, size, children` (what `file outline` prints) |
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
{"@unresolved":{"fixable_by":"agent","hint":"Check the id with 'agent-figma file outline <ref>'. Branch links need the branch key, and prototype or FigJam links may use different ids.","id":"99:99","reason":"Figma has no node 99:99 in this file"}}
```

Misses are not cached; asking for the same bad id again spends another Tier 1
call. Check ids with `file outline` or `nodes find` first.

### @related

Nodes worth reading next, so you do not have to explore: `rel` is
`ancestor`, `page`, `component_set`, `variant` (siblings in the set),
`main_component` (of an instance); `render` lines come from `render`. `of` is the printed node it
relates to. Prefer entries with `cost: "cached"`: reading them is free.

### Paging large subtrees (--max-nodes)

Output stops at `--max-nodes` records (default 400), chosen breadth-first so
the top of the tree survives. Cut records carry `truncated: true` and a
pagination line lists the nodes whose children were cut:

```bash
agent-figma nodes get FAKEFILEKEY0000000000000 --node 2:10 --max-nodes 4 --view structure
```

```json
{"id":"2:10","parent":"2:1","depth":0,"type":"FRAME","name":"Checkout","size":[375,940],"children":8,"truncated":true}
{"id":"2:11","parent":"2:10","depth":1,"type":"INSTANCE","name":"Button","size":[120,40],"children":1,"truncated":true}
{"id":"2:12","parent":"2:10","depth":1,"type":"TEXT","size":[327,24]}
{"id":"2:13","parent":"2:10","depth":1,"type":"RECTANGLE","name":"Hero image","size":[327,180]}
{"@pagination":{"has_more":true,"next_cursor":"2:10,2:11"}}
```

The whole subtree is already cached at this point, so continuing is free. Do
not pass the cursor back unchanged with the same `--max-nodes`: a listed
parent prints its already-shown children again. Instead either raise
`--max-nodes`, narrow with `--view` or `--depth`, or request the specific
subtrees you care about by id (`--node 2:11`).

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

`file index` caches only two levels from the root, so a following
`file outline` (three levels) spends its own Tier 1 call; after that both are
free. Use `--depth 1` for a quick page/frame list when budget is tight.

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
{"@coverage":{"incomplete":null,"matches":3,"searched":36}}
```

With less cached, `incomplete` names the frames still to deepen:

```json
{"@coverage":{"incomplete":["2:14","2:17","2:30"],"matches":0,"searched":24}}
```

`--name` is a case-insensitive glob or substring matched against layer
names (TEXT layers included, though `nodes get` hides their names); `--type` is a Figma node type (`FRAME`, `COMPONENT`, `COMPONENT_SET`,
`INSTANCE`, `TEXT`, …); `--limit` defaults to 100.

`@coverage` says how much was searched. `incomplete` lists frames whose
contents are **not** cached, so a miss there is not proof the node does not
exist: deepen those with `nodes get --node <id>` (batch them) and search
again.
