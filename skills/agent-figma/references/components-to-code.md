# Mapping components to code

`@components` says which design-system components a screen uses. To build
it you need the **code** component behind each one. That mapping belongs to
the codebase, not to a Figma account, so it lives in the repository and is
checked in:

```text
<repo-root>/.agent-figma/components.json
```

- **Found by walking up** from the working directory to the first directory
  holding `.agent-figma/`, so it works inside a monorepo package and outside
  git. With none, the file is proposed at the git root (or the working
  directory) and created by the first `map add` or `map suggest --write`.
  `--components-file <path>` or `AGENT_FIGMA_COMPONENTS` override.
- **Keyed by published keys**: `set_key` for a variant set, `key` for a
  standalone component. They are the same for every user, file, and token,
  so one file serves everyone in the repo; profile names never appear in it.
  `name` is the fallback for unpublished components (matched ignoring case,
  spaces, and emoji).
- Commands pick it up on their own; nothing to pass. Mapping never spends a
  Figma call.

## status: what is used, what is mapped

```bash
agent-figma components map status FAKEFILEKEY0000000000000 --node 2:10   # omit --node: every component the cached file uses
```

```json
{"@components_file":{"entries":0,"exists":false,"path":"/home/example/app/.agent-figma/components.json"}}
{"set":"Button","component_set_id":"1:10","set_key":"fakecomponentsetkey0000000000000000000110","count":2,"mapped":false,"variants":[{"props":{"Size":"md","State":"Default"},"count":1,"nodes":["2:11"]},{"props":{"Size":"md","State":"Hover"},"count":1,"nodes":["2:18"]}]}
{"@budget":{"cache_hits":1,"calls":{"tier1":0,"tier2":0,"tier3":0},"prefetched":{"nodes":0,"renders":0}}}
```

`@components_file` says which mapping file is in use and whether it exists.
Unmapped entries carry the `set_key` (or `key`) to map them by. Reads the
subtree cache-first, like `nodes get`.

## suggest: propose entries from the source tree

```bash
agent-figma components map suggest FAKEFILEKEY0000000000000 --node 2:10            # print proposals
agent-figma components map suggest FAKEFILEKEY0000000000000 --node 2:10 --write    # also save exact matches
agent-figma components map suggest FAKEFILEKEY0000000000000 --src packages/web     # scan another directory
```

```json
{"code":{"component":"Button","file":"src/components/ui/button.tsx","import":"src/components/ui/button"},"count":2,"match":"exact","set":"Button","set_key":"fakecomponentsetkey0000000000000000000110"}
{"@components_file":{"exports":2,"path":"/home/example/app/.agent-figma/components.json","scanned":"/home/example/app","written":0}}
```

It matches unmapped components' names against components exported from
`.tsx`, `.ts`, `.jsx`, `.js`, `.vue`, and `.svelte` files under `--src`
(default: the mapping's repository root). `match` is `exact` or `partial`
(e.g. `Button` → `ButtonGroup`); `--write` saves only exact matches
(`"written": true` on the line). The proposed `import` is the file path:
review it and re-add with your alias (`@/components/ui/button`) where the
codebase uses one. Treat partial matches as leads, not answers.

## add: write an entry

```bash
agent-figma components map add --set-key fakecomponentsetkey0000000000000000000110 --name Button \
  --component Button --import @/components/ui/button \
  --prop Size=size --value Size.md=default --ignore State \
  --notes "Disabled is the disabled prop, not a variant"
agent-figma components map add --key fakecomponentkey0000000000000000000000120 --name Icon/Star --component StarIcon --import @/components/ui/star-icon
agent-figma components map add --name "Promo banner" --component PromoBanner   # unpublished: by name
```

| Flag | Meaning |
|---|---|
| `--set-key K` / `--key K` / `--name N` | which Figma component (one is required; `--name` alongside a key is kept for readability) |
| `--component Name` | the code component (required) |
| `--import path` | its import path |
| `--prop Figma=code` | a Figma variant property's code prop name (repeatable) |
| `--value Prop.figma=code` | one value of that property in code (repeatable); unmapped values pass through |
| `--ignore Prop` | drop a property code handles itself, such as hover or pressed states (repeatable) |
| `--notes text` | anything an implementer should know |

It prints `{"entry": {...}, "path": "...", "status": "saved"}`. Adding an
entry with the same key (or, for keyless entries, the same name) **merges**
into it: flags you pass win, and earlier props, values, notes, and import are
kept. Under MCP, `map add` and `suggest --write` are refused: change the
mapping from a terminal in the repository. The file is sorted by name so reviews stay
small; editing it by hand is fine:

```json
{
  "version": 1,
  "components": [
    {
      "figma": {"set_key": "fakecomponentsetkey0000000000000000000110", "name": "Button"},
      "code": {"component": "Button", "import": "@/components/ui/button"},
      "props": {
        "Size": {"prop": "size", "values": {"md": "default"}},
        "State": {"ignore": true}
      },
      "notes": "Disabled is the disabled prop, not a variant"
    }
  ]
}
```

## What changes in output

Once the file exists, every `@components` entry in `nodes get` (including
`--view components`), `components map status`, and handoff's
`components.json` and `tree.ndjson` carries `mapped`, and mapped ones `code`
and per-variant `code_props`, as does `file outline`. A broken mapping file
does not stop reads: they print a notice and go unmapped, while
`components map …` commands report the problem:

```json
{"@components":[{"set":"Button","component_set_id":"1:10","set_key":"fakecomponentsetkey0000000000000000000110","count":2,"mapped":true,"code":{"component":"Button","import":"@/components/ui/button","notes":"Disabled is the disabled prop, not a variant"},"variants":[{"props":{"Size":"md","State":"Default"},"code_props":{"size":"default"},"count":1,"nodes":["2:11"]},{"props":{"Size":"md","State":"Hover"},"code_props":{"size":"default"},"count":1,"nodes":["2:18"]}]}]}
```

Instance records gain `code` with the call site's props mapped (non-variant
props such as text pass through under their Figma name):

```json
{"id":"2:11","parent":"2:10","depth":1,"type":"INSTANCE","name":"Button","size":[120,40],"component":{"id":"1:11","name":"State=Default, Size=md","set":"Button"},"code":{"component":"Button","import":"@/components/ui/button","notes":"Disabled is the disabled prop, not a variant","props":{"Label":"Pay now","size":"default"}},"props":{"Label":"Pay now","Size":"md","State":"Default"},"texts":["Pay now"],"instance":true,"children":1}
```

So `<Button size="default">Pay now</Button>` can be read straight off the
record. Without a mapping file, output has no `mapped` or `code` fields
(only `components map status` still says `"mapped": false`).

## Workflow

1. `components map status <ref> --node <screen>` to see what the screen uses.
2. `components map suggest <ref> --node <screen>` to propose matches; check
   them against the code, then `--write` or `map add` with the real import
   path and prop mappings.
3. Commit `.agent-figma/components.json` so the next person (and agent)
   gets mapped output.
4. `handoff` or `nodes get` now say what to import and with which props.
