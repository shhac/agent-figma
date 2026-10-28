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

- `notes.json` — designer notes and comments around the node (see Designer notes).

At most three Tier 1 calls (node subtree, PNG render, SVG render) and two Tier 2 (image fills, comments), each skipped when cached. The folder defaults to `<cache>/<profile>/<file>/v-<version>/handoff/<node-slug>--<id>/`; `--out` writes elsewhere (refused under MCP, where the files are read through the cache root). Stdout is one manifest line per file plus `@file` and `@budget`.

## 4. `compare`

```text
agent-figma compare <ref> --node id --image app.png [--scale auto] [--threshold 24]
```

Answers "does this match, and where not" from an implementation screenshot the agent took (for example with Playwright):

- The scale defaults to the screenshot's width over the node's width, so a 2x screenshot is compared with a 2x render. Figma renders include effects such as drop shadows (`absoluteRenderBounds`) while element screenshots usually do not, so both widths are tried and the result snapped to a usual scale (0.5, 1, 1.5, 2, 2.5, 3, 4) within 3%; otherwise the measured ratio is used. Without this, a frame with a drop shadow infers a slightly wrong scale, which misaligns the comparison and spends a Tier 1 call on a render at the wrong scale. When the screenshot is the size of the box, the render is cropped to the box before comparing. The render is cached like any other.
- Pixels are compared per channel against `--threshold` (0–255) after flattening transparency onto white. A size mismatch is reported and the overlapping area compared.
- Differing pixels are bucketed into a grid and merged into regions; each region is attributed to the deepest cached node whose box contains it, so the answer reads "the price text (2:33) differs", not "pixels at 410,212".
- Output: one record `{match_percent, design_size, image_size, scale, diff_image, regions: [{box, pixels, node: {id, type, name, text?}}]}`, where `diff_image` is a PNG highlighting differences over a faded design render.

`compare` spends at most one Tier 1 call (the render), plus one if the node is not cached.

## 5. Labels

Production files name their building blocks generically: many frames share one name like "Card", and wrappers are auto-named "Group 12". A node's `label` is the text of its most prominent heading, so an agent (and a human reading file names) sees "Order summary" rather than "Card".

- Computed for containers (`FRAME`, `GROUP`, `SECTION`, `COMPONENT`, `COMPONENT_SET`, `INSTANCE`) whose name is empty after auto-name stripping, is a generic word (card, container, content, wrapper, frame, group, section, box, item, row, column, stack, body, layout, block — after dropping emoji and punctuation), or is shared by three or more nodes in the output.
- The heading is the visible TEXT descendant with the largest font size (the first in depth order among equals), cut to 60 characters. No text, no label.
- Used in: node records (`label`), `file outline`, `nodes find` (matches name or label), `diff` paths, `handoff` folder names, and render, export, and handoff file names (`order-summary--2-30.png`).

## 6. Mapping components to code

`@components` says which design-system components a screen uses; building needs to know which *code* component each one is. That mapping belongs to a codebase, not to a Figma account, so it lives in the repository and is checked in:

```text
<repo-root>/.agent-figma/components.json
```

The file is found by walking up from the working directory to the first directory containing `.agent-figma/` (so it works inside a monorepo package, and outside git); `--components-file` or `AGENT_FIGMA_COMPONENTS` override.

**Keys, not ids or profiles.** A remote instance's `componentId` is a node id local to the *consuming* file, so it differs between files. The published `key` of the component and of its component set is global: the same for every user, file, and token, and present in every file response that uses it. Entries are keyed by `set_key` (variant sets) or `key` (standalone components), with `name` as a fallback for unpublished components. Nothing in the file mentions a profile, so two developers with differently named profiles, or tokens that can open different files, share one mapping.

```json
{
  "version": 1,
  "components": [
    {
      "figma": {"set_key": "fakecomponentsetkey0000000000000000000110", "name": "Button"},
      "code": {"component": "Button", "import": "@/components/ui/button"},
      "props": {
        "State": {"ignore": true},
        "Size": {"prop": "size", "values": {"md": "default", "sm": "small"}}
      },
      "notes": "Disabled is the disabled prop, not a variant"
    }
  ]
}
```

- `props` maps a Figma variant property to a code prop and, optionally, value by value; unmapped values pass through; `ignore` drops interaction states (hover, pressed) that code handles itself.
- Output: each `@components` entry gains `code: {component, import, notes?}` and `mapped: true`, and each variant `code_props`. Unmapped entries carry `mapped: false` and the `set_key`/`key` to add. Instance records gain `code` when mapped.
- `components map status <ref> [--node id]` lists the components a subtree uses, mapped or not, and which mapping file is in use. `components map add (--set-key|--key) <k> --component <Name> [--import <path>] [--prop Figma=code]... [--name <figma name>]` writes an entry (creating the file if needed). `components map suggest <ref> --node id [--src dir] [--write]` proposes entries for unmapped components by matching their names against components exported from source files under the repository (`.tsx`, `.ts`, `.jsx`, `.js`, `.vue`, `.svelte`), printing them for review.
- `handoff` writes the mapped view into `components.json`, so the folder says what to import.

## 7. Designer notes

Designs carry context outside the layers being built: sticky notes and loose text on the canvas beside a frame, text inside a section describing its flow, comments pinned to nodes ("truncate after two lines"), and component descriptions. Some is designer-to-designer, much is designer-to-implementer; all of it is worth surfacing, labelled by source, for the agent to weigh.

Sources, for a node:

- **canvas** — visible TEXT nodes and note-like nodes (names containing "sticky", "note", "annotation", "comment", "todo") on the page beside the node or its top-level ancestor, within `--radius` (default 400 px) of the node's box. Siblings inside frames and groups are the design itself, so only the page and sections are searched. Each carries `distance` and `position` (`above`, `below`, `left`, `right`, `overlapping`).
- **section** — text directly inside a `SECTION` that contains the node, at any distance: text in a section describes its contents.
- **comment** — Figma comments pinned to the node or a descendant (`client_meta.node_id`), with author, date, and resolved state, replies attached to their thread. One Tier 2 call.
- **description** — the descriptions of components the subtree uses, except keyword lists ("Close, Cancel, X"), which icon libraries use for search rather than guidance.
- **annotation** — Dev Mode annotations (`annotations[].label`) on the node or its descendants, when present.

`nodes notes <ref> --node id [--no-comments] [--radius 400]` prints one record per note, and `handoff` writes `notes.json` (with a count in the manifest). Canvas notes need the node's siblings cached; when they are not, `@notes_coverage` says so and suggests `file outline --node <parent>` (one Tier 1 call) rather than spending it silently.

## 8. Settings nudges

Figma reveals the seat class and the plan only in 429 headers, so a profile's declarations can be wrong for a long time before anything says so. When the tool learns something that contradicts a profile's settings, it says so with the exact command that fixes it:

- A 429 reporting `X-Figma-Rate-Limit-Type: low` on a profile declared `--seat full|dev`, or `high` on one declared `collab|view` → suggest `auth update <profile> --seat view` (or `--seat full`).
- A 429 reporting `X-Figma-Plan-Tier` different from the declared `--plan`, or with none declared → suggest `auth update <profile> --plan <reported>`.
- A `403 Invalid token` on a profile whose recorded expiry is still in the future → suggest `auth update <profile> --form --expires YYYY-MM-DD` (the token was revoked or the date is wrong).

Suggestions are `{reason, command}` records: printed as a stderr `notice` when the observation is made, folded into the hint of the error that revealed them, and (for seat and plan, which are recorded) listed under `suggestions` in `auth check` and `auth list` until fixed. A `--plan` nudge is withheld when Figma has also reported the declared plan for some file: one token can legitimately read files on several plans. They never change a profile by themselves: declarations are the user's statement of fact, and one file's plan can legitimately differ from the profile's usual one.
