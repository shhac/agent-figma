# Building and verifying UI from designs

What three agent tasks need from agent-figma, and the features that serve them:

- "Build this UI in my app" — the agent needs the structure, the design-system components and variants used, the styles, the assets, and a picture.
- "The designer updated the designs — what changed?" — the agent needs a precise list of changes, grouped by where they are, and before/after pictures of only what changed.
- "Does component X match the designs?" — the agent needs to know where an implementation differs from the design, not just whether it does.

Real files are large and system-driven: a single production page can hold tens of thousands of nodes, most of them instances of library components (icons, buttons, field parts) with values bound to design-system variables, while the frames built from them carry generic names and wrapper groups are auto-named. Every feature below is shaped by that, and by Tier 1 being a monthly allowance on Starter plans and low seats: each one leans on the version-keyed cache and batches what it must fetch.

## 1. Instances as leaves, and `@components`

An instance of a library component is, to an agent building UI, one call site with props — not the twenty nodes Figma draws it with. So in simplified output an `INSTANCE` is a leaf by default:

```json
{"id":"2:11","type":"INSTANCE","name":"Button","component":{"id":"1:11","name":"State=Default, Size=md","set":"Button"},"props":{"Label":"Pay now","Size":"md","State":"Default"},"texts":["Pay now"],"children":1,"instance":true}
```

- `texts` lists the visible text inside the instance (first 8, previews cut to 80 characters), so labels and copy survive without the internals.
- `instance: true` says the children were folded; `--expand-instances` emits them as before.
- A requested root that is itself an instance is always expanded: the agent asked to see inside it.
- Instances nested in a folded instance are folded with it.

`@components` summarises the design-system usage of everything emitted, one entry per component set (or standalone component):

```json
{"@components":[{"set":"Button","component_set_id":"1:10","remote":false,"count":2,"variants":[{"props":{"Size":"md","State":"Default"},"count":1,"nodes":["2:11"]},{"props":{"Size":"md","State":"Hover"},"count":1,"nodes":["2:18"]}]}]}
```

Variant props come from the instance's `componentProperties` (VARIANT values only, so the key is stable); entries are ordered by count, and each variant lists up to 10 node ids. `--view components` prints only this summary (as records), for "what does this screen use" without the tree.

## 2. `diff`

```text
agent-figma diff <ref> [--node id...] [--from <version>] [--to <version>] [--render] [--max-changes 200]
```

- `--to` defaults to the current version (checked with `/meta`, fetched if the subtree is not cached at it: one Tier 1 call).
- `--from` defaults to the newest *older* cached version that holds the subtree, so after any earlier read, "what changed?" costs at most one call. An explicit `--from <version id>` (from `versions list`) that is not cached is fetched with `?version=` (one more Tier 1 call).
- Without `--node`, the roots are the file's pages and the diff runs over whatever both versions have cached under them; `@diff.coverage` says which top-level frames were compared, and the hint suggests `--node` for a full comparison of one area.

Nodes are matched by id. Each is compared on its simplified form (styles resolved to values, instances folded): `name`, `text`, `size`, `pos`, `layout`, `fills`, `strokes`, `effects`, `radius`, `opacity`, `component`, `props`, `texts`, `text_style`, `svg`, and its parent. Records:

```json
{"change":"changed","id":"2:12","type":"TEXT","path":"Checkout › Total","fields":{"text":{"from":"Total: **$42.00**","to":"Total: **$50.00**"}}}
{"change":"added","id":"5:1","type":"FRAME","name":"Promo","path":"Checkout","descendants":3}
{"change":"removed","id":"2:16","type":"RECTANGLE","path":"Checkout"}
{"change":"moved","id":"2:13","path":"Checkout › Card","fields":{"parent":{"from":"2:10","to":"2:30"}}}
```

An added or removed subtree is one record with a `descendants` count, not one per node. `path` is the chain of meaningful ancestor names (auto-named wrappers skipped) under the diff root. Then `@diff` summarises: `{from, to, added, removed, changed, moved, frames: [{id, name, changes}]}` where frames are the nearest top-level frames containing changes.

`--render` renders the changed frames (up to 20) at both versions — one render call per version, batched, using the images endpoint's `version` parameter — and lists `{id, name, before, after}` file pairs. Before-renders already cached cost nothing.

## 3. `handoff`

```text
agent-figma handoff <ref> --node id [--out dir] [--scale 2]
```

One command that leaves everything needed to build a screen in one folder, so an agent does not spend five round trips assembling it:

- `screenshot.png` — the node at `--scale` (one Tier 1 render call, cached).
- `tree.ndjson` — simplified nodes, instances folded, with `@styles` and `@components` lines.
- `components.json`, `styles.json` — the same summaries as documents.
- `icons/<name>.svg` — every `svg: true` node in the subtree, deduplicated by main component (an icon used 300 times is exported once), rendered in one SVG call.
- `images/<image_ref>.<ext>` — image fills (one Tier 2 call for the whole file's map, cached).
- `manifest.json` — what is where, the file version, and the budget spent.

At most three Tier 1 calls (node subtree, PNG render, SVG render) and one Tier 2, each skipped when cached. The folder defaults to `<cache>/<profile>/<file>/v-<version>/handoff/<node-slug>--<id>/`; `--out` writes elsewhere (refused under MCP, where the files are read through the cache root). Stdout is one manifest line per file plus `@file` and `@budget`.

## 4. `compare`

```text
agent-figma compare <ref> --node id --image app.png [--scale auto] [--threshold 24]
```

Answers "does this match, and where not" from an implementation screenshot the agent took (for example with Playwright):

- The scale defaults to the screenshot's width over the node's width, so a 2x screenshot is compared with a 2x render. The render is cached like any other.
- Pixels are compared per channel against `--threshold` (0–255) after flattening transparency onto white. A size mismatch is reported and the overlapping area compared.
- Differing pixels are bucketed into a grid and merged into regions; each region is attributed to the deepest cached node whose box contains it, so the answer reads "the price text (2:33) differs", not "pixels at 410,212".
- Output: one record `{match_percent, design_size, image_size, scale, diff_image, regions: [{box, pixels, node: {id, type, name, text?}}]}`, where `diff_image` is a PNG highlighting differences over a faded design render.

`compare` spends at most one Tier 1 call (the render), plus one if the node is not cached.

## 5. Labels (later)

Generically named frames get a `label` from their largest heading text, used in outlines, `find`, paths, and file names. Deferred until after the four above.
