# Errors and notices

Errors: one JSON object on stderr, non-zero exit, nothing useful on stdout.

```json
{"error":"…","fixable_by":"agent|human|retry","hint":"…","retry_after_seconds":600}
```

- `agent`: the input is wrong; fix it and re-run.
- `human`: a person must act (token, scope, seat, plan, file access). Tell the
  user what the hint says; do not retry in a loop.
- `retry`: transient; wait `retry_after_seconds` (when present) and re-run.

Per-item misses are **not** errors: `@unresolved` lines on stdout, exit 0
(unknown node ids, nodes that render as nothing, unknown library keys).

Notices: `{"notice","hint"}` on stderr, exit unaffected. Informational
(token expiring, Tier 1 monthly count, a depth-limited fallback).

## Common errors

| Error (start of `error`) | `fixable_by` | What to do |
|---|---|---|
| `Figma tier1 rate limit reached; retry in …` with "low (Collab/View seat)" in the hint | human | Monthly Tier 1 allowance spent. Stop Tier 1 commands, use `--offline` for cached data, prefer `file index` / library lists, tell the user |
| `Figma tierN rate limit reached; retry in …` | retry | Wait `retry_after_seconds`. "No request was sent" in the hint means another process's cooldown; same advice |
| `Figma rejected the token: Invalid token` | human | Token wrong or expired. User makes a new one and runs `agent-figma auth update <profile> --form` |
| `The token is missing a scope for this request` | human | User regenerates the token with the scope named; `agent-figma usage` lists the recommended ones |
| `Figma limits this endpoint by plan or seat` | human | Variables are Enterprise-only. Use `styles list` and `@styles` names instead |
| `Permission denied` | human | The token's user cannot open this file; ask the user to check sharing |
| `Not found: …` | agent | Wrong key or a link from another account. Check with `resolve`; branch links are routed automatically, but a bare branch key cannot list library items |
| `Figma refused the request as too large` | agent | Ask for a narrower `--node` (a child frame) or a smaller `--depth` |
| `nothing cached for this file and --offline forbids calling Figma` | agent | Drop `--offline`, or check `cache status` for the profile and key |
| `profile "x" is not configured` | agent | `agent-figma auth list`; pick an existing profile or ask the user to add one |
| `assets export needs at least one node id` (likewise `render`) | agent | Pass `--node <id>` or a link with `?node-id=` |
| `--scale must be between 0.01 and 4` | agent | Fix the value; svg/pdf ignore scale anyway |
| `--out is not available through the MCP server` | agent | Under MCP, read the manifest `file` reference with the `fs` tool |
| `Figma server error (5xx)` | retry | Already retried `--max-retries` times; re-run later |

## Example: low-seat 429

```json
{"error":"Figma tier1 rate limit reached; retry in 10m0s","fixable_by":"human","hint":"Figma reports a low (Collab/View seat) limit: Tier 1 calls are a small monthly allowance, so waiting may not help; a Full or Dev seat raises it. Plan tier: starter. Upgrade: https://www.figma.com/settings Data already cached for this file can still be read with --offline.","retry_after_seconds":600}
```

After this, `serve_stale: auto` treats the profile as a low seat: commands
answer from an older cached version when they can (`@file.stale: true`).

## Notices worth passing on

| Notice | Meaning |
|---|---|
| `The token for profile "x" expires in N day(s)` / `expired on …` | User should rotate it: `auth update x --form --expires YYYY-MM-DD` |
| `Spent a Tier 1 call on a monthly-limited budget (N this month …)` | Low seat or Starter file; keep Tier 1 calls to a minimum |
| `Figma refused the full subtree of … as too large; showing a depth-limited tree` | Output stops early; records marked `truncated` need their own `nodes get --node` |
| `Could not check the file's version with /meta, so the cache was not validated` | Usually a missing `file_metadata:read` scope; answers may be older than the file |
| `Could not re-check the file's version; answering from the last known version` | Network or budget trouble on the freshness check; the answer may be stale |
