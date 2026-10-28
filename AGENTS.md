# agent-figma

Figma CLI for AI agents: read file and node metadata, render node images, and download image assets with compact, LLM-shaped output and no direct access to Figma tokens. Go + cobra.

## Project Rules

- **No real Figma identifiers anywhere in the repo.** File keys, branch keys, node ids, team/folder ids, user ids, file or page names, and figma.com links taken from a real account must never appear in code, comments, docs, the skill, mocks, fixtures, tests, examples, or commit messages. Use obviously synthetic values instead (e.g. file key `FAKEFILEKEY0000000000000`, node ids like `1:2`, names like `Example File`). Anything that needs a real file reads it from an environment variable at runtime (see Live tests).
- Stay consistent with the sibling `agent-*` CLIs (`agent-stripe`, `agent-slack`, …): same layout (`cmd/`, `internal/`, `design-docs/`, `skills/`), same Makefile targets, same output and error contract. Use the shared `lib-agent-*` Go libraries wherever they cover the need instead of re-implementing:
  - `lib-agent-cli` — root command, persistent flags, config, credentials, `--form` dialogs, XDG directories (`xdg`) for config, cache, and state.
  - `lib-agent-output` — NDJSON results, `@`-prefixed metadata lines, `{error, fixable_by, hint}` errors.
  - `lib-agent-mcp` — the `mcp` command over the cobra tree.
  - `lib-agent-keyring` (via `lib-agent-cli`) — OS secret storage.
- Lists default to NDJSON. Errors are JSON on stderr with `fixable_by` (`agent` | `human` | `retry`) and a `hint`. Never exit with an unstructured error.
- Never print, log, or persist raw Figma tokens outside the credential backend; scrub them from error bodies and `--debug` output. Prefer `agent-figma auth add <profile> --form` when guiding a user through setup; do not ask the user to paste tokens into chat.
- Read-only by default. Any future write (e.g. posting comments) must be gated behind `--yes` and approved in a design doc first.
- Tier 1 Figma calls (file, nodes, image renders) are the scarce budget. Do not add a code path that spends one without going through the cache, the batching/prefetch planner, and the shared rate-limit handling. The plan tier and seat type behind a profile's limits are detected and cached per profile (and can be set during `auth add`), never assumed.
- Keep Figma HTTP logic dependency-injected so tests run without network access; back CLI contract tests with the `mockfigma` fixture server (synthetic data only).

## Workflow

- Commit directly to `main` and push `main`. Local branches are fine for parallel work (g2g can manage them), but never push them: merge into `main`, then push `main`.
- Use conventional commit messages (`feat: …`, `fix(cache): …`).
- **Before building something**, make sure `design-docs/` describes it: the decision, the command surface, and anything a reviewer can't see from the code. Update the doc in the same change if the design shifts while building.
- **After building something**, make sure nothing user- or agent-facing is stale (see Keeping docs in sync).
- Order of work: design doc → auth + API client → cache + rate limiting → file/node commands → render + assets → prefetch → skill + MCP polish.

## Verification

```bash
make test     # go test ./... -count=1
make vet
make lint     # golangci-lint v2
```

Run all three before every commit.

### Live tests

Tests that talk to the real Figma API are opt-in and skip cleanly when their environment is missing. Never assume a future runner has the same Figma account, plan, or access as the author:

- `FIGMA_TOKEN` — the token to use (never a stored profile).
- `AGENT_FIGMA_LIVE_URL` — a Figma design URL (optionally with `node-id`) the token can read.

Live tests must be cheap in Tier 1 calls and must not hard-code anything about the file they read.

## Keeping docs in sync

When you add or change commands, flags, output shapes, or notable behavior, update all of these in the same change:

- **`agent-figma usage`** — the top-level reference card compiled into the binary.
- **`skills/agent-figma/`** — the LLM-facing skill, using incremental disclosure: `SKILL.md` stays short (what the tool is for, the output contract, the common paths, and a router), and detail lives in `references/` files loaded only when needed. A command or flag change touches the matching reference file.
- **`design-docs/`** — record the decision, not just the code. `initial-design.md` covers the core; `build-and-verify.md` covers the instance folding, `diff`, `handoff`, and `compare` features; `mock-figma.md` covers the mock.
- **`README.md`** — when the change is user-facing.
