# Building, tracking changes, and verifying

Commands for the jobs agents are usually asked to do with a design.
All examples use the synthetic file `FAKEFILEKEY0000000000000`.

## "Build this UI in my app" — `handoff`

```bash
agent-figma handoff 'https://www.figma.com/design/FAKEFILEKEY0000000000000/Example-File?node-id=2-10' --out ./design/checkout
```

Writes one folder and prints one line per file (`kind`, `path`, and a cache
`file` reference):

| File | Use it for |
|---|---|
| `screenshot.png` | what it should look like (2x by default, `--scale`) |
| `tree.ndjson` | structure: `@styles`, `@components`, then one simplified record per node, instances folded |
| `components.json` | which design-system components and variants to use, with node ids, and with a [mapping file](components-to-code.md) the code component, import, and `code_props` |
| `styles.json` | named colours, type styles, effects — map to your theme tokens |
| `icons/*.svg` | every vector icon, once per component |
| `images/*` | image fills (photos, illustrations) |
| `notes.json` | `{coverage, notes}`: what designers left around the node (see [nodes notes](#anything-the-designer-said-about-this--nodes-notes)) |
| `manifest.json` | the above plus file version, the node, the note count, and the budget spent |

Costs at most 3 Tier 1 calls (subtree, screenshot, icons) and 2 Tier 2
(image fills, comments); cached parts are free, so a second handoff of the
same node costs nothing. Build from `components.json` first (with no mapping
yet, map each set to your codebase's component: `components map suggest`),
read `notes.json` for behaviour the pixels don't show, then lay out from
`tree.ndjson`, then check against `screenshot.png`.

Without `--out` the folder lands in the cache, named by the node's label
when its name is generic (`handoff/42-00--2-30/` for a frame named `Card`
whose heading is `$42.00`), and each line carries a `file` reference for the
MCP `fs` tool. With `--out`, `path` is relative to the working directory.

## "Anything the designer said about this?" — `nodes notes`

```bash
agent-figma nodes notes FAKEFILEKEY0000000000000 --node 2:10
```

```json
{"source":"canvas","text":"Hand-off: Pay stays disabled until a card is entered.","node_id":"7:2","name":"Sticky note","position":"above","distance":40}
{"source":"canvas","text":"Totals include tax; round to cents.","node_id":"7:1","name":"Note","position":"right","distance":45}
{"source":"description","text":"Primary action button.","node_id":"2:11","name":"Button"}
{"source":"comment","text":"Should the price row wrap on narrow screens?","node_id":"2:32","author":"Example Designer","created_at":"2026-01-13T09:15:00Z","replies":[{"source":"comment","text":"Yes, let it wrap below 320px.","author":"Example User","created_at":"2026-01-13T10:02:00Z"}]}
{"@budget":{"cache_hits":1,"calls":{"tier1":0,"tier2":1,"tier3":0},"prefetched":{"nodes":0,"renders":0}}}
```

| `source` | What it is |
|---|---|
| `canvas` | visible text and note-like frames (names with sticky, note, annotation, comment, todo) beside the node or its ancestors, within `--radius` px (default 400); `position` is `above`, `below`, `left`, `right`, or `overlapping`, `distance` in px |
| `section` | text directly inside a `SECTION` containing the node, at any distance (it describes the section's flow) |
| `comment` | Figma comments pinned to the node or anything inside it: `author`, `created_at`, `resolved`, `replies`. One Tier 2 call, cached; `--no-comments` skips it |
| `description` | descriptions of the components the subtree uses (keyword lists such as icon search terms are left out) |
| `annotation` | Dev Mode annotations on the node or its descendants |

Some notes are designer-to-designer, some are for whoever builds it: read
them all and weigh them by source. A pinned comment or a sticky note beside
the frame usually outranks a component description.

Canvas notes need the node's siblings (and its ancestors' siblings) cached.
When they are not, no Tier 1 call is spent silently; instead:

```json
{"@notes_coverage":{"complete":false,"hint":"Notes beside the node may be missing: outline its parent with 'agent-figma file outline <ref> --node 2:10 --depth 1' (one Tier 1 call), then ask again.","uncached":["2:10","2:1"]}}
```

Every id in `uncached` needs outlining; do them in one call
(`file outline <ref> --node 2:10,2:1 --depth 1`) rather than one hint at a
time. A `file index` or default `file outline` beforehand usually covers
top-level frames already. `handoff` writes the same records to `notes.json`.

## "The designer updated the designs — what changed?" — `diff`

```bash
agent-figma diff FAKEFILEKEY0000000000000 --node 2:10
```

```json
{"change":"changed","id":"2:12","type":"TEXT","path":"Checkout","fields":{"text":{"from":"Total: **$42.00**","to":"Total: **$50.00**"}}}
{"change":"changed","id":"2:34","type":"ELLIPSE","name":"Status dot","path":"Checkout › $42.00 › Card body › Price row","fields":{"fills":{"from":"#22C55E","to":"#FF0000"}}}
{"change":"added","id":"9:1","type":"FRAME","name":"Promo","path":"Checkout","descendants":1}
{"change":"removed","id":"2:13","type":"RECTANGLE","name":"Hero image","path":"Checkout"}
{"@diff":{"from":"1000000000000000001","to":"1000000000000000002","added":1,"removed":1,"changed":2,"moved":0,"frames":[{"id":"2:12","changes":1},{"id":"2:30","name":"$42.00","changes":1},{"id":"9:1","name":"Promo","changes":1},{"id":"2:13","name":"Hero image","changes":1}]}}
```

- The baseline is the newest older version in the cache, so any earlier read
  of the node is enough; the diff then costs one Tier 1 call (the current
  version), and repeating it costs nothing.
- No baseline cached → the error says to pass `--from <version id>`; get ids
  from `versions list` (who and when). That costs one more Tier 1 call.
- Without `--node`, it compares every top-level frame cached in full at the
  baseline.
- `--render` adds `@render {id, name, before, after}` image pairs for the
  frames with changes (one render call per version).
- Added or removed subtrees are one record with `descendants`; `changed`
  lists only the fields that differ (`text`, `fills`, `layout`, `size`,
  `component`, `props`, ...), values as in `nodes get`.
- `path` is the chain of ancestor names under the diff root, auto-named
  wrappers skipped and generic names replaced by their label (`$42.00` above
  is the frame named `Card`), so look-alike frames stay apart.

## "Does my component match the design?" — `compare`

```bash
# 1. screenshot just the component in the running app, e.g. Playwright:
#    await page.locator('[data-testid=card]').screenshot({ path: 'card.png' })
# 2. compare it with the design node
agent-figma compare FAKEFILEKEY0000000000000 --node 2:30 --image card.png
```

```json
{"id":"2:30","name":"Card","scale":2,"match_percent":99.74,"mismatched_pixels":256,"design_size":[654,240],"image_size":[654,240],
 "regions":[{"box":{"x":576,"y":96,"w":16,"h":16},"pixels":256,"node":{"id":"2:34","type":"ELLIPSE","name":"Status dot"}}],
 "diff_image":{"path":"/.../compare/2-30@2x-diff.png", "file":{...}}}
```

- The scale is inferred from the screenshot (2x for a retina screenshot);
  Figma renders include shadows, element screenshots usually don't, and both
  are handled.
- Each region names the deepest design node under it — fix that node's
  styles, then compare again (the render is cached, so re-checking is free).
- `diff_image` shows the design faded with differences in red (and any size
  mismatch in orange). Open it to see what is off.
- `--threshold` (default 24 per channel) ignores anti-aliasing noise; lower
  it to catch subtle colour drift.
- For exact values rather than pixels, compare `nodes get --node <id>` (sizes,
  padding, gap, colours, type) with the implementation's computed styles.
