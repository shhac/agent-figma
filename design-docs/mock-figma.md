# mockfigma

`internal/mockfigma` is a local stand-in for the Figma REST API that backs the CLI contract tests; `cmd/mockfigma` serves it on `127.0.0.1:12112` (`make mock`, `make mock-dev ARGS="..."`, `--addr`, `--routes`). Everything in it is synthetic: file key `FAKEFILEKEY0000000000000`, branch key `FAKEBRANCHKEY00000000000`, image refs `fakeimageref000N`, names like `Example File`.

Tests use `mockfigma.New()` (an `http.Handler`) inside `httptest.NewServer`. The server builds every URL it hands out (render, fill, thumbnail, avatar, pagination) from the request's `Host`, so links point back at whichever listener served them. `Stats()`, `Reset()`, `SetFaults(spec)`, `BumpVersion(key)` and `Edit(key, edits...)` give in-process tests the same controls as the HTTP endpoints. The fixture's node ids, keys and versions are exported constants (`NodeCheckout`, `ButtonDefaultKey`, `InitialVersion`, …).

## Fixture

One document; the branch key starts from the same tree with its own name (`Example Branch`) and version history, and is edited independently.

```text
0:0  DOCUMENT
├── 0:1  CANVAS "Cover"
│   └── 0:2  FRAME "Cover" (IMAGE fill fakeimageref0002, FIT) → 0:3 TEXT (text style Heading/H1)
├── 1:1  CANVAS "Components"
│   ├── 1:10 COMPONENT_SET "Button" (componentPropertyDefinitions: State, Size, Label#1:5)
│   │   ├── 1:11 COMPONENT "State=Default, Size=md"  ┐ horizontal auto layout, padding, itemSpacing,
│   │   ├── 1:12 COMPONENT "State=Hover, Size=md"    │ SOLID fill bound to Brand/Primary, cornerRadius,
│   │   └── 1:13 COMPONENT "State=Disabled, Size=md" ┘ TEXT "Label" (Body/Label); Hover has a shadow, Disabled opacity 0.5
│   └── 1:20 COMPONENT "Icon/Star" → 1:21 VECTOR, 1:22 ELLIPSE
└── 2:1  CANVAS "Screens"
    └── 2:10 FRAME "Checkout" (vertical auto layout)
        ├── 2:11 INSTANCE of 1:11 (componentProperties, overrides) → I2:11;1:14 TEXT
        ├── 2:12 TEXT "Total: $42.00" (characterStyleOverrides + styleOverrideTable bold run)
        ├── 2:13 RECTANGLE (IMAGE fill fakeimageref0001, FILL)
        ├── 2:14 FRAME "Frame 12" (no auto layout) → 2:20 RECTANGLE constrained top-right
        ├── 2:15 GROUP "Logo" → 2:21, 2:22 VECTOR
        ├── 2:16 RECTANGLE visible:false
        ├── 2:17 FRAME "Export me" (GRADIENT_LINEAR, exportSettings PNG@2x + SVG@1x) → 2:23
        ├── 2:18 INSTANCE of 1:12
        └── 2:30 FRAME "Card" (strokes, effect style) → 2:31 "Card body" → 2:32 "Price row" → 2:33 TEXT, 2:34 ELLIPSE (layoutPositioning ABSOLUTE)
```

Local styles: `9:1` Brand/Primary (FILL), `9:2` Heading/H1 (TEXT), `9:3` Body/Label (TEXT), `9:4` Elevation/Card (EFFECT). All four components and the set are published, as are the four styles. One variable collection (`Tokens`, Light/Dark) holds a color, a float, and an alias; two of them are published.

Versions are numeric strings, newest first: main starts at `1000000000000000001` with four older versions; the branch at `1000000000000000101`. Every version of the starting history serves the fixture above.

### Versions are snapshots

Each version carries its own document tree. A read with `version=<id>` serves that version's tree; without it, the newest. Published trees are never mutated: an edit deep-copies the newest tree, changes the copy, and publishes it as a new version, so a render URL or a cached response for an older version keeps showing the old content. The `components` / `componentSets` / `styles` maps are built from whichever tree was served. The published-library endpoints (`/components`, `/styles`, `/v1/components/:key`, …) describe the fixture as built and ignore edits, as a library only changes on publish.

Two ways to make a version, both one higher and one minute later than the newest:

- `BumpVersion(key)` / `POST /_mock/version/bump[?key=]`: a new version with the same content (an edit that changes nothing a read can see).
- `Edit(key, edits...)` / `POST /_mock/edit[?key=]`: a new version whose tree is the newest tree with the edits applied in order. One call is one version, however many edits it carries. If any edit fails, nothing is published.

`Reset()` / `POST /_mock/reset` restores both original histories, discarding every edit.

### Edits

`Edit{Op, Node, Field, Value, Parent, Index, NodeJSON}`; over HTTP the body is a JSON list of objects with snake_case names (`op`, `node`, `field`, `value`, `parent`, `index`, `node_json`), and an unknown name is a 400.

| `Op` | Uses | Effect |
|---|---|---|
| `EditSet` `"set"` | `Node`, `Field`, `Value` | sets one top-level property to a JSON value (`characters`, `fills`, `itemSpacing`, `absoluteBoundingBox`, …). `name` takes a string; `id`, `type`, `children` cannot be set. Setting `absoluteBoundingBox` on a vector-like node refreshes its `geometry=paths` output. |
| `EditRemove` `"remove"` | `Node` | deletes the node and its subtree (not the document) |
| `EditAdd` `"add"` | `Parent`, `Index`, `NodeJSON` | inserts a Figma-shaped node object (`id`, `type`, optional `name`, `children`, any other properties; `fillGeometry`/`strokeGeometry` or derived from the box) under a container `Parent`. Every id in it must be new. |
| `EditMove` `"move"` | `Node`, `Parent`, `Index` | reparents the node under a container `Parent`, never under itself |

`Index` is the position among `Parent`'s children (for a move, counted after the node has left its old parent); negative (`AppendIndex`) or past the end appends, and an omitted JSON `index` inserts first. Go values are round-tripped through JSON, so `[]map[string]any` and ints take the fixture's own shapes.

```text
POST /_mock/edit?key=FAKEFILEKEY0000000000000
[{"op": "set", "node": "2:12", "field": "characters", "value": "Total: $50.00"},
 {"op": "set", "node": "2:34", "field": "fills", "value": [{"type": "SOLID", "color": {"r": 1, "g": 0, "b": 0, "a": 1}}]},
 {"op": "add", "parent": "2:10", "index": -1, "node_json": {"id": "3:1", "type": "RECTANGLE", "absoluteBoundingBox": {"x": 24, "y": 840, "width": 327, "height": 40}}}]
→ 200 {"key": "FAKEFILEKEY0000000000000", "version": "1000000000000000002", "lastModified": "…"}
```

An unknown node or parent id, unknown op, read-only field, non-container parent, clashing id, cycle, or malformed body is a `400 {"error": …}` (a failed edit reads `edit <i> (<op>): …`); an empty list is a 400 too, and an unknown key a 404.

## Routes

All `GET`. API routes need a non-empty `X-Figma-Token` or `Authorization: Bearer`; the token `expired-token` gets `403 Invalid token`. An unknown file key is `404 Not found`.

| Route | Notes |
|---|---|
| `/v1/me` | synthetic user with `email` |
| `/v1/files/:key` | `ids`, `depth`, `version`, `geometry=paths`, `branch_data` |
| `/v1/files/:key/nodes` | `ids` (required), `depth`, `version`, `geometry` |
| `/v1/images/:key` | `ids`, `scale` (0.01–4), `format`, svg/bounds flags accepted, `version` |
| `/v1/files/:key/images` | `{error, status, meta: {images}}` |
| `/v1/files/:key/meta` | `{file: {...}}`, `url` built from the synthetic key |
| `/v1/files/:key/versions` | `page_size` (1–50, default 30), `before`, `after` |
| `/v1/files/:key/comments` | a thread pinned to `2:32` (`client_meta.node_id`) plus a canvas comment |
| `/v1/files/:key/components`, `/component_sets`, `/styles` | main key only |
| `/v1/components/:key`, `/v1/component_sets/:key`, `/v1/styles/:key` | one published item |
| `/v1/files/:key/variables/local`, `/published` | |
| `/v1/files/:key/dev_resources` | `node_ids`; main key only |

Semantics worth knowing:

- **`ids` on `/v1/files/:key`** returns each listed node's full subtree, the local main components their instances use (with those components' own ancestor chains), and the ancestor chain from the root. Ancestors carry only the children on a path, and every top-level CANVAS is present (an off-path page has `children: []`). Unknown ids are left out.
- **`depth`** is a positive integer. On `/v1/files/:key` it counts from the root (`depth=1` is pages only); on `/nodes` it counts from each requested node. A container cut by the limit has no `children` key at all, so "not fetched" differs from "empty".
- **`components` / `componentSets` / `styles`** maps on file and node responses list only what the returned nodes contain or reference.
- **`/nodes`** maps an unknown id to `null`.
- **`version`** on file, nodes and images serves that version's snapshot and reports it as `version`; an id the history lacks is a 404. `/meta` always reports the newest.
- **`/v1/images/:key`** includes every requested id; unknown ids, the document, and nodes that are invisible (or inside an invisible parent) in the requested version map to `null`. URLs point at `/_cdn/render/<id>.<format>?key=<key>&scale=<scale>&version=<version>` with `:` and `;` percent-escaped in the id. The response is `{err: null, images}` with no `status` field (Figma's docs say it is omitted on success).
- **Branch keys** work everywhere except the library lists and dev resources, which answer `404` because they need the main key.
- **Error bodies** follow the OpenAPI spec per endpoint: `{status, err}` for most; `{error: true, status, message}` for variables and dev resources, and for the library endpoints' 404.
- Raw `;` in a query string (instance ids such as `I2:11;1:14`) is dropped by Go's parser; send it percent-encoded, as `url.Values` does.

`/_cdn/<kind>/<name>.<ext>` needs no auth and serves a valid 1×1 PNG, a 1×1 JPEG, a one-rect SVG, or a one-page PDF with the matching `Content-Type`; any other extension is a 404.

### Sized renders

PNG and JPEG renders (`/_cdn/render/<id>.png|.jpg`) are drawn, so `compare`-style tests can match or mismatch a screenshot and localise a difference. The query names the snapshot: `key` (default the main file), `version` (default the newest) and `scale` (default 1); an unknown key, version or node is a 404. The drawing is a deterministic stand-in for Figma's renderer, predictable from the tree alone:

- Size is `round(width × scale)` × `round(height × scale)` from the node's `absoluteBoundingBox`, each side clamped to 1..`MaxRenderSide` (4096). A node without a box (a page) is 1×1.
- The background is the node's first visible `SOLID` fill, or white.
- Every visible descendant (a hidden node hides its subtree) with a visible `SOLID` fill is drawn, in tree order, as an opaque rectangle at its box relative to the node's, scaled, with edges rounded and clipped to the image. A TEXT node is a rectangle of its text colour.
- Alpha, opacity, gradients, image fills, strokes, effects, corner radii and clipping are ignored.

So an edit to one child's fill changes exactly the pixels of that child's rectangle. JPEGs are quality 95, so their colours are close rather than exact. SVG and PDF renders stay the fixed tiny files.

## Mock controls

No auth needed:

- `GET /` lists the routes (also `mockfigma --routes`).
- `GET /_mock/stats` returns `{"tier1": n, "tier2": n, "tier3": n, "by_path": {path: n}, "requests": [{"path", "query"}]}`. It counts every request under `/v1/` or `/v2/`, classified by `ratelimit.TierFor`, including ones a fault or a bad token refused, since a real client spent budget on them. CDN downloads and mock controls are not counted.
- `POST /_mock/reset` clears counters and faults and restores both original version histories (edits are discarded).
- `POST /_mock/faults?rules=<comma-separated>` replaces the standing rules (a rule that does not parse is a 400 naming it); `GET /_mock/faults` shows them with how often each fired; `DELETE /_mock/faults` clears them.
- `POST /_mock/version/bump[?key=<key>]` adds a newest version with unchanged content.
- `POST /_mock/edit[?key=<key>]` with a JSON list of edits adds a newest version with them applied (see Edits). Both answer `{key, version, lastModified}`; `key` defaults to the main file.

## Fault grammar

A rule is colon-separated positional parts followed by `key=value` options; options are recognised by `=`, so node ids keep their colons. `count=<n>` limits how often a rule fires (default unlimited).

| Rule | Effect |
|---|---|
| `429:<tier1\|tier2\|tier3\|/path-prefix>[:retry=<s>][:type=low\|high][:plan=<tier>]` | 429 with `Retry-After` (default 1), `X-Figma-Plan-Tier` (default `pro`; `enterprise`, `org`, `pro`, `starter`, `student`), `X-Figma-Rate-Limit-Type` (default `high`), `X-Figma-Upgrade-Link` |
| `too-large:<path-prefix>[:min_nodes=<n>]` | `400 Request too large. If applicable, filter by query params.` on file/nodes responses that would carry at least `min_nodes` nodes (default 0). `400-too-large` is an alias. |
| `render-timeout[:min_ids=<n>]` | `400 Render timeout, try requesting fewer or smaller images` on image calls with at least `min_ids` ids |
| `render-500` | `500` on image calls |
| `null-render:<node-id>` | that id renders as `null` |
| `status:<code>:<path-prefix>` | any 4xx/5xx with Figma's error body |
| `plan-limited:variables` | `403 Limited by Figma plan` on both variables endpoints |
| `cdn-expired[:<path-prefix>]` | the CDN answers 403 with an S3-style `AccessDenied` / `Request has expired` XML body |

Rules apply in the order given; the first live match wins. A request can carry its own rules in an `X-Mock-Fault` header instead of the standing ones; their counts are tracked by rule text, so repeating the header continues the count.
