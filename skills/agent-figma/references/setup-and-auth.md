# Setup and auth

## Creating a token (the human does this)

1. In Figma: Settings > Security > Personal access tokens > Generate new token.
2. Give it read-only scopes: `file_content:read`, `file_metadata:read`,
   `library_content:read`, `library_assets:read`, `team_library_content:read`,
   `file_versions:read`, `file_comments:read`, `file_dev_resources:read`,
   `current_user:read`, and `file_variables:read` on Enterprise. Not the
   deprecated `files:read`.
3. Note the expiry date (personal access tokens last at most 90 days).
4. Run, in their own terminal:

```bash
agent-figma auth add work --form --expires 2026-12-31 --seat dev --default
```

`--form` opens a native OS dialog; the token goes straight into the OS
keychain. **Never ask the user to paste a token into chat, and never pass one
with `--token` yourself.** Piping it on stdin (`... | agent-figma auth add work`)
also works for scripted setups.

Receipt:

```json
{"declared_plan":"unknown","declared_seat":"dev","default":true,"expires":"2026-12-31","expires_in_days":64,"kind":"pat","profile":"work","status":"added","storage":"keychain"}
```

## Flags on `auth add` / `auth update`

| Flag | Meaning |
|---|---|
| `--form` | prompt for the token in a native dialog |
| `--kind pat\|plan` | personal access token (default) or plan access token (Org/Enterprise admins; up to 365 days) |
| `--expires YYYY-MM-DD` | record the expiry; commands warn when fewer than 7 days remain |
| `--clear-expires` | (`update` only) forget the expiry |
| `--seat full\|dev\|collab\|view\|unknown` | declare the seat; `collab`/`view` mean a monthly Tier 1 allowance |
| `--plan starter\|pro\|org\|enterprise\|student\|unknown` | the plan you mostly read files from |
| `--default` | make it the default profile |

Seat and plan are optional. The CLI also learns them from Figma's 429
responses and keeps both (see [rate-limits-and-cache.md](rate-limits-and-cache.md#seats-and-plans)).

## Checking and managing profiles

```bash
agent-figma auth check            # default profile; Tier 3 call to /v1/me
agent-figma auth check work --file "<ref>"   # plan access tokens cannot call /v1/me: verify with a file
agent-figma auth list             # profiles, declared/observed seat and plan, expiry; never the token
agent-figma auth default work
agent-figma auth update work --form --expires 2027-03-01   # rotate the token
agent-figma auth remove old       # removes the profile, its token, and its cache
```

`auth check` output:

```json
{"budget":{"effective_seat_class":"high","tier1_calls_this_month":0},"declared_plan":"org","declared_seat":"dev","expires":"2026-11-01","expires_in_days":4,"kind":"pat","profile":"work","status":"ok","token_source":"profile","user":{"email":"example.user@example.com","handle":"Example User","id":"100000000000000002"}}
```

After a 429, `budget` also shows `observed_rate_limit_type`, `observed_at`, and
active `cooldowns` (keyed `<plan>/<tier>`).

## Which token and profile a command uses

Token: `--token` → `AGENT_FIGMA_TOKEN` → the selected profile's stored token
→ `FIGMA_TOKEN`. `FIGMA_TOKEN` is last on purpose: other Figma tools export
it, and it must not override a profile set up here. `token_source` in
`auth check` says which one won.

Profile: `--profile/-p` → `AGENT_FIGMA_PROFILE` → the default profile. The
cache and rate-limit state are kept per profile; a token from an environment
variable uses the cache scope `env`.

## Storage

Profile metadata lives in `$XDG_CONFIG_HOME/agent-figma/config.json`; tokens
are in the OS keychain (service `app.paulie.agent-figma`), falling back to a
0600 `credentials.json` when no keychain is available
(`AGENT_FIGMA_NO_KEYCHAIN=1`). Tokens are never printed, logged, or written
into error bodies or `--debug` output.

## Expiry

When a profile's recorded expiry is under 7 days away, every command emits a
notice on stderr:

```json
{"hint":"Generate a new token in Figma and run 'agent-figma auth update work --form --expires YYYY-MM-DD'.","notice":"The token for profile \"work\" expires in 4 day(s), on 2026-11-01"}
```

Pass it on to the user; the command itself still ran.
