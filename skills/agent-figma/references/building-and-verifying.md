# Building, tracking changes, and verifying

Three commands for the jobs agents are usually asked to do with a design.
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
| `components.json` | which design-system components and variants to use, with node ids |
| `styles.json` | named colours, type styles, effects — map to your theme tokens |
| `icons/*.svg` | every vector icon, once per component |
| `images/*` | image fills (photos, illustrations) |
| `manifest.json` | the above plus file version and the budget spent |

Costs at most 3 Tier 1 calls (subtree, screenshot, icons) and 1 Tier 2
(fills); cached parts are free, so a second handoff of the same node costs
nothing. Build from `components.json` first (map each set to your codebase's
component and its props), then lay out from `tree.ndjson`, then check
against `screenshot.png`.

## "The designer updated the designs — what changed?" — `diff`

```bash
agent-figma diff FAKEFILEKEY0000000000000 --node 2:10
```

```json
{"change":"changed","id":"2:12","type":"TEXT","path":"Checkout","fields":{"text":{"from":"Total: **$42.00**","to":"Total: $50.00"}}}
{"change":"removed","id":"2:13","type":"RECTANGLE","name":"Hero image","path":"Checkout"}
{"change":"added","id":"9:1","type":"FRAME","name":"Promo","path":"Checkout","descendants":1}
{"@diff":{"from":"1000000000000000001","to":"1000000000000000002","added":1,"removed":1,"changed":1,"moved":0,"frames":[...]}}
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
