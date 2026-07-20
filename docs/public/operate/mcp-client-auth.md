# MCP Client Authentication

`eshu mcp setup` is posture-aware (issue #5169, F-8): it detects which
credential story your deployment's MCP endpoint actually uses and emits a
matching client snippet, instead of a single hard-coded shared-key example.
This page explains the posture model, the exact shape each client gets, and
walks the three deployment shapes end to end: a token-only org, an Okta SSO
org, and a GitHub org.

## Posture model

Eshu derives one auth posture per deployment — never a second switch an
operator has to keep in sync:

- **token** — per-user bearer tokens (the default everywhere; issue #5164).
  Always available, regardless of whether SSO is configured.
- **sso** — OAuth 2.1 via the [RFC 9728 discovery
  document](mcp-oauth-discovery.md), when an identity provider is
  configured for MCP.
- **shared-key** — the legacy admin/dev `ESHU_API_KEY`. Never the default;
  only ever emitted behind an explicit flag.

`eshu mcp setup --hosted` detects the posture automatically: it sends one
unauthenticated, short-timeout `GET` to
`{service-url}/.well-known/oauth-protected-resource` before printing
anything. A `200` with a non-empty `authorization_servers` list is the only
signal that maps to SSO — the discovery handler never serves an empty issuer
list, so a `200` is proof the OAuth flow can actually complete. Every other
outcome — `404` (the documented "no active bearer issuer" signal), a network
error, a timeout, or a malformed response — maps to token posture. A
misdetection can therefore only ever fall *toward* a configuration that still
authenticates (per-user tokens exist under every posture); it can never
silently emit an OAuth-only config against a token-only deployment. On a
fallback, the command prints a warning naming the cause and the override:

```
warning: could not verify auth posture (...); emitting per-user token config.
If this deployment uses SSO for MCP, re-run with --auth sso.
```

Override the detection with `--auth auto|sso|token|shared-key`. `--shared-key`
remains available as a standalone boolean for discoverability and is
equivalent to `--auth shared-key`. Local stdio setup (no `--hosted`) never
probes — stdio spawns a local process and carries no bearer credential either
way.

**Posture flip mid-session.** The discovery document is cacheable for 60
seconds and the emitted snippet is static text, not a live lookup. Enabling
an IdP after tokens were already issued does not break emitted token
configs — tokens keep working, SSO is additive. Removing an IdP *does* break
an already-emitted SSO config; re-run `eshu mcp setup` to get a fresh token
snippet.

## Client matrix

| Client | Token posture | SSO posture | Shared key |
| --- | --- | --- | --- |
| Claude Code | `headers: { Authorization: Bearer ${ESHU_MCP_TOKEN} }` | OAuth via `/mcp` (RFC 9728 discovery, no `headers` key) | `headers` with `${ESHU_API_KEY}`, admin/dev only |
| claude.ai connector | **unsupported** (OAuth-only client) | OAuth (needs a DCR-capable broker or the claude.ai callback registered on the IdP app) | unsupported |
| VS Code / Cursor / generic | `headers` with an env-var reference | header-less entry — the client follows the 401 challenge | `headers` with `${ESHU_API_KEY}` |
| Codex CLI | `bearer_token_env_var = "ESHU_MCP_TOKEN"` | URL only; fall back to `--auth token` if the client cannot run the OAuth flow | `bearer_token_env_var = "ESHU_API_KEY"` |
| stdio local | no credential | no credential | n/a |

No posture ever prints a raw secret: every shape above is an environment
variable *reference*, never an inlined value. In every hosted posture the
emitted URL is the service base plus the fixed MCP endpoint path,
`/mcp/message` — only the credential wiring changes between postures.

## Token-only org: first five minutes

1. Bring the stack up (`docker compose up --build`, or a deployed
   `mcp-server`).
2. Complete the [console first-run
   flow](../getting-started/console-first-five-minutes.md) to claim the
   bootstrap owner.
3. Open `/profile` → **API tokens** → create a personal token.
4. Export it: `export ESHU_MCP_TOKEN=...`
5. Run `eshu mcp setup --hosted --platform claude` (or `cursor`, `vscode`,
   `codex`, `generic`) and paste the printed snippet into your client's
   config.
6. In Claude Code, run `/mcp` and confirm the `eshu` tool call succeeds.

This org has no identity provider configured, so the discovery probe
correctly `404`s and `eshu mcp setup` auto-detects token posture. Token
issuance and use are attributed per user in the `token_lifecycle` and
`api_mcp_authentication` audit families — see [User Management Runbook §Tokens
And Sessions](user-management-runbook.md#tokens-and-sessions).

## Okta org

The technical contract here matches [MCP OAuth 2.1
Discovery](mcp-oauth-discovery.md) exactly — this section only walks the
`eshu mcp setup` experience on top of it:

- Okta is a **custom authorization server**: the access token's `aud` claim
  must equal `ESHU_AUTH_RESOURCE_URI` (RFC 8707).
- Okta offers **no anonymous dynamic client registration and no CIMD**, so
  either pre-register a native/public PKCE client
  (`ESHU_AUTH_PREREGISTERED_CLIENT_ID`) or put a DCR/CIMD-capable broker in
  front.
- claude.ai connectors need either that broker or the claude.ai callback
  registered on the Okta application.
- The `groups` scope is load-bearing: the resolver denies a token that
  resolves to no group-derived grant.
- **`require_sso` only hides local password *login*.** Personal tokens,
  service principals, and the shared break-glass key keep working — SSO adds
  an OAuth path, it never removes the token paths.

With the IdP configured, `eshu mcp setup --hosted --platform claude` probes
the discovery document, gets a `200` with a non-empty `authorization_servers`
list, and prints a header-less snippet naming the issuer and (when
configured) the pre-registered client id. In Claude Code, `/mcp` → choose
`eshu` completes the browser sign-in.

Proof checklist for the MCP lane specifically: 401 challenge on an
unauthenticated request → discovery document fetch → browser OAuth flow →
successful tool call. See [User Management Runbook §Okta Test
Flows](user-management-runbook.md#okta-test-flows) for the broader OIDC/SAML
proof checklist; the scripted end-to-end MCP OAuth proof is F-9 (#5170).

## GitHub org

GitHub sign-in (issue #5166, F-5) is **plain OAuth2, not an MCP authorization
server** — there is no discovery document, no protected-resource metadata,
and no bearer-token validation path for GitHub-issued credentials. The
dashboard uses "Continue with GitHub" for browser login, but **MCP always
uses personal tokens** on a GitHub-only org.

This means the discovery probe correctly `404`s here too, exactly like a
token-only org: `eshu mcp setup` auto-detects token posture. This is the
designed behavior, not a bug or a gap to close — GitHub's OAuth2 flow simply
has no MCP-side authorization-server role to advertise. `allowed_orgs` in the
GitHub provider config file remains the tenant boundary regardless of MCP
credential posture (see `ESHU_AUTH_GITHUB_CONFIG_FILE` in the [Environment
Variable Registry](../reference/env-registry.md)).

## Shared key

The legacy shared `ESHU_API_KEY` is an admin/dev credential: full
`AllScopes` access with no per-user attribution. `eshu mcp setup` never
emits it by default under any posture — only an explicit `--auth shared-key`
or `--shared-key` does, and the printed snippet always carries this warning:

```
WARNING: the shared ESHU_API_KEY is an admin/dev credential: full AllScopes
access with no user attribution. Use it for bootstrap and break-glass only.
Per-user tokens (the default) or SSO are the supported paths for engineers.
```

Reserve it for local bootstrap and break-glass access, not day-to-day
engineer setup.

## Troubleshooting

**Probe fell back to token but you expected SSO.** Three known causes, all
covered by [MCP OAuth 2.1 Discovery](mcp-oauth-discovery.md):

- An ingress or reverse proxy routes only `/mcp/*` to `eshu-mcp-server` and
  not `/.well-known/*`. Route the well-known path per RFC 9728 — real MCP
  clients need this routing independent of the CLI.
- The bearer-resolver issuer snapshot has a short startup TTL window before
  the first refresh; a probe during that window sees zero active issuers.
  Retry after the snapshot populates.
- `ESHU_AUTH_RESOURCE_URI` is unset or fails the `https://` (or loopback
  `http://`) validation the discovery handler requires.

**The fallback output still works.** Token posture always authenticates —
per-user tokens exist regardless of whether SSO is configured — so a
misdetected fallback degrades to "prints a working token snippet," never to
"prints a broken one."
