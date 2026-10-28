# Rate limits and cache

## Tiers

Figma meters **requests** (not node ids) per endpoint tier, in a leaky bucket
per user and per plan of the file being read. Budgets depend on the token
owner's seat and on that file's plan:

| Tier | Endpoints (commands) | Full/Dev seat: Pro / Org / Enterprise | Collab/View seat (any plan), or a Starter file |
|---|---|---|---|
| 1 | file, nodes, image renders (`file outline`, `nodes get`, `render`, `assets export`, the outline in `file index`) | 10 / 15 / 20 per min | a handful **per month** |
| 2 | image fills, comments, versions, variables, dev resources | 25 / 50 / 100 per min | 5 per min |
| 3 | file meta, components, component sets, styles, `/v1/me` | 50 / 100 / 150 per min | 10 per min |

So, in order of preference: an answer from the cache (`cost: cached`), a
known render URL (`cost: download`, CDN only), a Tier 3 call, a Tier 2 call,
and last a Tier 1 call (`cost: tier1`; `@related` entries only ever cost
`cached`, `download`, or `tier1`). Every read of a file ends with `@budget`
saying what it spent:

```json
{"@budget":{"cache_hits":1,"calls":{"tier1":0,"tier2":0,"tier3":0},"prefetched":{"nodes":0,"renders":0}}}
```

On a low (Collab/View) seat or a Starter file, every Tier 1 call also emits
a notice with the running monthly count:

```json
{"notice":"Spent a Tier 1 call on a monthly-limited budget (3 this month for this profile)","hint":"Figma allows Collab/View seats and Starter files only a few Tier 1 calls a month; prefer cached reads and Tier 3 commands like 'file index'."}
```

## Seats and plans

Figma's API does not report the seat type, and the plan that matters is the
**file owner's**. So both come from two places, kept side by side:

- **Declared**: `auth add|update <profile> --seat full|dev|collab|view --plan starter|pro|org|enterprise`.
- **Observed**: every 429 records Figma's `X-Figma-Rate-Limit-Type` (`low` =
  Collab/View, `high` = Full/Dev) for the profile and `X-Figma-Plan-Tier`
  for that file.

The effective seat class is the declaration, else the observation, else
`high`. The effective plan for a file is the observation for that file, else
the declaration, else `pro`. `auth check` and `auth list` show
`declared_seat`, `declared_plan`, `effective_seat_class`,
`observed_rate_limit_type`, `tier1_calls_this_month`, and active `cooldowns`.

If the user mentions a View/Collab seat, suggest they declare it
(`auth update <profile> --seat view`), so the CLI is careful from the first
call instead of after the first 429.

## 429s and cooldowns

- Requests are paced to the per-minute budget above. Tier 1 on a low seat or
  Starter file is not paced (the budget is monthly), only counted.
- A 429 waits `Retry-After` if that is within `--max-wait` (default 60s,
  config `max_wait_s`) and retries; 429 waits do not use `--max-retries`.
- A longer wait fails fast with `retry_after_seconds`. The cooldown is stored
  in shared state, so **parallel agent-figma processes respect it too**: they
  fail without calling Figma (`"No request was sent: an earlier 429 started a
  cooldown …"`).
- `fixable_by: retry`: wait `retry_after_seconds`, then re-run.
- `fixable_by: human` with a "low (Collab/View seat)" hint: the monthly
  allowance is spent; waiting may not help. Stop spending Tier 1, answer
  from the cache (`--offline`), and tell the user a Full or Dev seat raises
  the limit.
- 5xx, 408, and network errors retry with backoff up to `--max-retries`
  (default 2).

## Freshness and stale answers

Each command first decides which file version is current: within
`cache_fresh_s` (default 300s) of the last check it trusts the cached
version; otherwise it asks `/meta` (Tier 3) and compares.

When that check finds the file changed and nothing is cached for the new
version, but an older version could answer, `serve_stale` decides:

| `serve_stale` | Behaviour |
|---|---|
| `auto` (default) | `on` where Tier 1 is monthly (low seats, Starter files), `off` otherwise |
| `on` | answer from the older version |
| `off` | refetch (Tier 1) |

A stale answer is marked on `@file`:

```json
{"@file":{"key":"FAKEFILEKEY0000000000000","name":"Example File","version":"1000000000000000001","last_modified":"2026-01-15T10:00:00Z","editor_type":"figma","stale":true,"latest_version":"1000000000000000003"}}
```

Mention it to the user when recency matters (a designer is editing now).

Per-command overrides:

- `--refresh`: re-check the version now (ignores `cache_fresh_s`) and answer
  from the current version, fetching it if it is not cached. It never gives
  a stale answer, even with `serve_stale on`. Stale answers only come from
  the routine version check above.
- `--offline`: never call Figma. Answers from the newest cached version for
  this profile and key, or fails with `fixable_by: agent` ("nothing cached
  for this file and --offline forbids calling Figma").

## Prefetch

Because cost is per request, Tier 1 calls are widened with ids the cache
already knows you will probably want next: the component set and sibling
variants of a requested variant (nodes, at most 10 extra ids), and sibling
variants at the same format and scale (renders, at most 24). Extras are
cached and offered as `@related`, never printed as records; `@budget.prefetched`
counts them. If a widened call fails it is retried once with only the
requested ids.

Turn it off with `--no-prefetch` (one command) or `config set prefetch off`.

## Cache

Per profile, per file, per version, under `$XDG_CACHE_HOME/agent-figma/<scope>/`
(the scope is the profile name, or `env` for a token from an environment
variable). It holds the node store, render and fill URL maps (render URLs
are reused for 29 days, fill URLs for 13), the library lists, and downloaded
files. The newest 3 versions per file are kept.

```bash
agent-figma cache status                         # every cached file for this profile
agent-figma cache status FAKEFILEKEY0000000000000
agent-figma cache path
agent-figma cache clear FAKEFILEKEY0000000000000  # one file
agent-figma cache clear --all                    # everything for this profile
```

```json
{"bytes":10301,"checked_at":"2026-10-28T12:15:43Z","current_version":"1000000000000000001","file_key":"FAKEFILEKEY0000000000000","name":"Example File","nodes":31,"path":"/home/example/.cache/agent-figma/work/FAKEFILEKEY0000000000000","scope":"work","versions":["1000000000000000001"]}
```

Clearing the cache is never needed for correctness (versions keep it
honest); it throws away budget already spent, so do it only when asked.

## Config keys

```bash
agent-figma config list
agent-figma config set serve_stale on
agent-figma config unset serve_stale
```

| Key | Default | Meaning |
|---|---|---|
| `cache_fresh_s` | 300 | seconds a file's cached version is trusted before re-checking |
| `prefetch` | `related` | `related` widens Tier 1 calls; `off` asks only for what was requested |
| `serve_stale` | `auto` | `auto`, `on`, `off` (above) |
| `max_wait_s` | 60 | longest rate-limit wait to sit out |
| `max_retries` | 2 | retries for 5xx and network failures |
| `timeout_ms` | 60000 | command timeout |

Flags override config for one command (`--max-wait`, `--max-retries`,
`--timeout`, `--no-prefetch`).
