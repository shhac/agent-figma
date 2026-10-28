# Library and other reads

## Links and keys

`resolve` parses links offline (no API call, no token needed):

```bash
agent-figma resolve "https://www.figma.com/design/FAKEFILEKEY0000000000000/branch/FAKEBRANCHKEY00000000000/Example-File?node-id=2-10"
```

```json
{"api_key":"FAKEBRANCHKEY00000000000","branch_key":"FAKEBRANCHKEY00000000000","editor_path":"design","file_key":"FAKEFILEKEY0000000000000","input":"https://www.figma.com/design/FAKEFILEKEY0000000000000/branch/FAKEBRANCHKEY00000000000/Example-File?node-id=2-10","node_ids":["2:10"]}
{"api_key":"FAKEFILEKEY0000000000000","file_key":"FAKEFILEKEY0000000000000","input":"FAKEFILEKEY0000000000000"}
```

Fields that do not apply are left out: a bare key has no `editor_path`,
`branch_key`, or `node_ids`.

**Branch vs main key.** A `/branch/<key>` link has two keys. Every command
routes automatically:

- File, nodes, renders, image fills, meta, versions, and comments use the
  branch key (`api_key`): you see the branch's content and history.
- Library lists (`components`, `component-sets`, `styles`) and
  `dev-resources` use the main `file_key`, because Figma only serves them for
  the main file.

A bare key is used as given everywhere: a bare branch key sent to a library
command or `dev-resources` comes back `Not found`, so pass the main key or
the branch link instead.

## file meta

```bash
agent-figma file meta FAKEFILEKEY0000000000000     # Tier 3, cheap
```

```json
{"branch_key":"","editor_type":"figma","folder_name":"Example Project","key":"FAKEFILEKEY0000000000000","last_touched_at":"2026-01-15T10:00:00Z","last_touched_by":"Example Designer","link_access":"view","name":"Example File","role":"owner","thumbnail_url":"…","version":"1000000000000000001"}
```

The file's current version, name, and a thumbnail URL. Use it to confirm
access or see whether a file changed.

## file index

The catalogue to start with. Tier 3 calls (meta, published components,
component sets, styles) plus the page/frame outline, which is one Tier 1 call
unless already cached (`--no-outline` skips it).

```bash
agent-figma file index FAKEFILEKEY0000000000000
```

Records have a `kind`:

```json
{"frames":2,"id":"1:1","kind":"page","name":"Components"}
{"children":3,"id":"1:10","kind":"frame","name":"Button","page":"Components","size":[424,72],"type":"COMPONENT_SET"}
{"description":"Primary action button.","file_key":"FAKEFILEKEY0000000000000","key":"fakecomponentsetkey0000000000000000000110","kind":"component_set","name":"Button","node_id":"1:10","page":"Components","thumbnail_url":"…","updated_at":"2026-01-12T12:00:00Z","variants":[{"key":"fakecomponentkey0000000000000000000000111","node_id":"1:11","props":{"Size":"md","State":"Default"},"thumbnail_url":"…"}]}
{"description":"Five-point star icon.","key":"fakecomponentkey0000000000000000000000120","kind":"component","name":"Icon/Star","node_id":"1:20","page":"Components","thumbnail_url":"…"}
{"description":"Primary brand colour.","key":"fakestylekey0000000000000000000000000091","kind":"style","name":"Brand/Primary","node_id":"9:1","style_type":"FILL","thumbnail_url":"…"}
{"@budget":{"cache_hits":0,"calls":{"tier1":1,"tier2":0,"tier3":4},"prefetched":{"nodes":0,"renders":0}}}
```

The outline it caches is the depth a default `file outline` prints, so
`file outline` straight after `file index` is free.

Published components and variants carry `thumbnail_url`, a small preview you
can look at without spending a render. Unpublished components appear only
once their nodes are cached (with `"published": false`).

## Components, component sets, styles

All Tier 3 and cached per file version. `list` takes a ref (main key; branch
links are routed to it); `get` takes published keys (from `list` or `index`)
and returns one line per key, `@unresolved` for misses, exit 0.

```bash
agent-figma components list FAKEFILEKEY0000000000000
agent-figma component-sets list FAKEFILEKEY0000000000000
agent-figma styles list FAKEFILEKEY0000000000000
agent-figma components get fakecomponentkey0000000000000000000000111
agent-figma styles get fakestylekey0000000000000000000000000091
```

```json
{"file_key":"FAKEFILEKEY0000000000000","frame":"Button","key":"fakecomponentkey0000000000000000000000111","name":"State=Default, Size=md","node_id":"1:11","page":"Components","set":"Button","set_node_id":"1:10","thumbnail_url":"…","updated_at":"2026-01-12T12:00:00Z"}
{"description":"Primary brand colour.","file_key":"FAKEFILEKEY0000000000000","key":"fakestylekey0000000000000000000000000091","name":"Brand/Primary","node_id":"9:1","style_type":"FILL","thumbnail_url":"…","updated_at":"2026-01-12T12:00:00Z"}
```

`node_id` is what `nodes get` / `render` take; `key` is the published
library key, only for `get`. `style_type` is `FILL|TEXT|EFFECT|GRID`. A key
`get` does not know is a per-key miss, exit 0:

```json
{"@unresolved":{"id":"fakecomponentkey0000000000000000000009999","reason":"Not found","fixable_by":"agent","hint":"Check the key with 'agent-figma components|component-sets|styles list <ref>'; keys are not node ids."}}
```

## Variables

**Enterprise plans only** (and the token needs `file_variables:read`). Tier 2.

```bash
agent-figma variables list FAKEFILEKEY0000000000000              # local variables, values per mode
agent-figma variables list FAKEFILEKEY0000000000000 --published  # what this file publishes
```

```json
{"default_mode":"Light","id":"VariableCollectionId:8:0","key":"fakevariablecollectionkey0001","kind":"collection","modes":[{"id":"8:0","name":"Light"},{"id":"8:1","name":"Dark"}],"name":"Tokens"}
{"code_syntax":{"WEB":"var(--color-brand)"},"collection":"Tokens","collection_id":"VariableCollectionId:8:0","description":"Brand colour.","id":"VariableID:8:1","key":"fakevariablekey0001","kind":"variable","name":"color/brand","scopes":["ALL_FILLS"],"type":"color","values":{"Dark":"#758FFF","Light":"#3B5BF5"}}
{"collection":"Tokens","collection_id":"VariableCollectionId:8:0","id":"VariableID:8:2","key":"fakevariablekey0002","kind":"variable","name":"spacing/md","scopes":["GAP"],"type":"float","values":{"Dark":16,"Light":16}}
{"collection":"Tokens","collection_id":"VariableCollectionId:8:0","description":"Alias of color/brand.","id":"VariableID:8:3","key":"fakevariablekey0003","kind":"variable","name":"color/action","scopes":["ALL_FILLS"],"type":"color","values":{"Dark":{"alias":"VariableID:8:1"},"Light":{"alias":"VariableID:8:1"}}}
{"@budget":{"cache_hits":0,"calls":{"tier1":0,"tier2":1,"tier3":0},"prefetched":{"nodes":0,"renders":0}}}
```

Records are snake_case like node records, and empty fields are left out:

- **collection**: `{kind: "collection", id, name, key, modes: [{id, name}], default_mode, remote?, hidden_from_publishing?}`. `default_mode` is the mode's name.
- **variable**: `{kind: "variable", id, name, key, collection_id, collection, type, description?, scopes, remote?, code_syntax?, values}`. `type` is lowercase (`color`, `float`, `string`, `boolean`); `collection` is the collection's name.
- `values` is keyed by **mode name**. Colours are CSS (`#RRGGBB`, or `rgba(…)` when not opaque), numbers, strings, and booleans are plain, and an alias is `{"alias": "<variable id>"}`: resolve it by finding that `id` in the list.

`--published` lists what the file publishes, with fewer fields (no modes or
values). On other plans the command fails with `fixable_by: human`
("Limited by Figma plan"): fall back to `styles list` and the `@styles`
names in `nodes get`.

## Comments

```bash
agent-figma comments list FAKEFILEKEY0000000000000           # Tier 2
agent-figma comments list FAKEFILEKEY0000000000000 --as-md   # message as markdown
```

```json
{"created_at":"2026-01-13T09:15:00Z","id":"100001","message":"Should the price row wrap on narrow screens?","node_id":"2:32","order_id":"1","user":"Example Designer"}
{"created_at":"2026-01-13T10:02:00Z","id":"100002","message":"Yes, let it wrap below 320px.","node_id":"2:32","parent_id":"100001","user":"Example User"}
{"created_at":"2026-01-14T11:00:00Z","id":"100003","message":"Cover copy is final.","order_id":"2","resolved_at":"2026-01-14T15:30:00Z","user":"Example User"}
{"@budget":{"cache_hits":0,"calls":{"tier1":0,"tier2":1,"tier3":0},"prefetched":{"nodes":0,"renders":0}}}
```

`node_id` is the node the comment is pinned to (absent for canvas comments);
`parent_id` marks a reply; `resolved_at` a resolved thread. Read-only:
posting comments is not supported.

## Versions

Tier 2, newest first, paginated with Figma's cursor.

```bash
agent-figma versions list FAKEFILEKEY0000000000000 --limit 2
agent-figma versions list FAKEFILEKEY0000000000000 --limit 2 --cursor 1000000000000000000
```

```json
{"created_at":"2026-01-15T10:00:00Z","description":"Adds the card and price row.","id":"1000000000000000001","label":"Checkout v2","user":"Example Designer"}
{"created_at":"2026-01-14T16:30:00Z","id":"1000000000000000000","user":"Example Designer"}
{"@pagination":{"has_more":true,"next_cursor":"1000000000000000000"}}
{"@budget":{"cache_hits":0,"calls":{"tier1":0,"tier2":1,"tier3":0},"prefetched":{"nodes":0,"renders":0}}}
```

Pass `next_cursor` back as `--cursor` until `@pagination` stops appearing.
`--limit` is at most 50 (default 30). Labelled versions (`label`) are the
named milestones designers saved.

## Dev resources

Links attached to nodes in Dev Mode (Storybook, docs, tickets). Tier 2; main
file key only (branch links are routed to it).

```bash
agent-figma dev-resources list FAKEFILEKEY0000000000000
agent-figma dev-resources list FAKEFILEKEY0000000000000 --node 1:10
```

```json
{"file_key":"FAKEFILEKEY0000000000000","id":"fakedevresource0001","name":"Button in Storybook","node_id":"1:10","url":"https://example.com/storybook/button"}
{"file_key":"FAKEFILEKEY0000000000000","id":"fakedevresource0002","name":"Checkout spec","node_id":"2:10","url":"https://example.com/docs/checkout"}
{"@budget":{"cache_hits":0,"calls":{"tier1":0,"tier2":1,"tier3":0},"prefetched":{"nodes":0,"renders":0}}}
```

Only the `@budget` line and exit 0 means there are none.

## Raw API

`api get` is a GET-only escape hatch for endpoints without a command. It
goes through the same rate limiter and retries, and prints Figma's JSON
unchanged. Tier 1 paths (`/v1/files/<key>`, `/nodes`, `/v1/images/<key>`)
spend the scarce budget **and bypass the cache**: prefer `nodes get` and
`render`.

```bash
agent-figma api get /v1/files/FAKEFILEKEY0000000000000/versions --param page_size=1
```
