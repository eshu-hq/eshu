# MCP-identity auth E2E suite (F-9, #5170)

A sibling of the #4971 browser-auth suite (`runAuthE2E.ts`), scoped to the
**MCP HTTP transport** identity story of epic #5161: it proves auth follows the
configured identity story on `GET /sse`, `POST /mcp/message`, and
`/.well-known/oauth-protected-resource` across three org shapes, plus a
negative-leakage module.

Run it (owns the `docker-compose.e2e.yaml` stack lifecycle on an isolated
project + 29xxx ports). The stack's graph is Neo4j (the digest-pinned
`docker-compose.neo4j.yml` image, no published ports); the runner seeds it with
`docker compose exec neo4j cypher-shell`:

```bash
bash scripts/run-auth-mcp-e2e.sh
```

The wrapper builds the exact-source `eshu` CLI once in an owned temporary
directory before it starts the browser runner. Credential retrieval and the MCP
setup-posture check then call that binary directly; the credential read has a
15-second runtime timeout, so a cold Go compile cannot be misreported as a
Postgres outage. Cleanup removes the temporary binary whether the gate passes,
fails, or keeps the Compose stack for debugging.

## Module map

| File | Role |
| --- | --- |
| `runAuthMcpE2E.ts` | Orchestrator. Bootstraps the first-run wizard, seeds one graph repository, then runs each shape module and the leakage module in the load-bearing order A → C → B → leakage. Also has a standalone `credentialless` fast path (`ESHU_E2E_MCP_MODULE=credentialless`) with no browser/wizard, used by the sensitivity gate. |
| `authMcpE2ETokenFlow.ts` | Shape A (token-only). |
| `authMcpE2EGithubFlow.ts` | Shape C (GitHub, stubbed against `mock-github`). |
| `authMcpE2EShapeB.ts` | Shape B (OIDC via the mock IdP): provider CRUD, static-discovery assertion (no live flip under static config), precedence regression, `require_sso` flip. |
| `authMcpE2EOauthClient.ts` | Pure-`fetch` scripted RFC 9728 + RFC 7636 (PKCE) OAuth client — MCP OAuth is a machine flow, no browser. Includes the in-network→localhost URL rewrite the browser gets via Chromium `--host-resolver-rules`. |
| `authMcpE2ELeakage.ts` + `authMcpE2ELeakageDenials.ts` | Negative-leakage module: credential-less probes, the distinct bad-credential denial matrix, the non-vacuous cross-scope row filter, and the raw-token-absence scan. |
| `authMcpE2EGraphSeed.ts` | Seeds one `Repository` node into Neo4j with `docker compose exec neo4j cypher-shell`, and parses the resolver's denial-outcome log lines. |
| `authMcpE2EJsonRpc.ts` / `authMcpE2EPsql.ts` | Shared JSON-RPC-over-HTTP client and psql helpers. |

Verifiers: `scripts/verify-auth-mcp-e2e-manifest.sh` (report vs
`testdata/golden/auth-mcp-e2e-baseline.json`) and
`scripts/verify-auth-mcp-e2e-sensitivity.sh` (mutation-sensitivity of the
negative module). Both run in the `auth-mcp-e2e` CI job
(`.github/workflows/frontend.yml`, registered in `specs/ci-gates.v1.yaml`).

## Three adaptations worth knowing

- **Row-filter dimension is scope grant, not tenant.** `list_indexed_repositories`
  is graph-backed here, and repositories carry no `tenant_id` (only `id`/`name`).
  The non-vacuous row-filter proof seeds one graph node and shows it is visible
  to an AllScopes cookie session but filtered to zero for a scoped (empty-grant)
  bearer. No MCP *bearer* credential in this stack is AllScopes — personal
  identity tokens and OIDC bearers both resolve to `AuthModeScoped` with an
  empty grant — so the AllScopes reader is shape B's surviving SSO-admin cookie
  session.
- **Denial distinctness comes from edge logs + challenge shape.** The matrix
  asserts the satellite edge's structured-log `outcome` (`unknown_issuer` /
  `wrong_audience` / `malformed` on the `oidc Bearer denied` line — the same
  message and fields core's oidcbearer resolver emits) and the RFC 9728
  challenge shape (pre-match → `resource_metadata`, post-match → bare
  `Bearer`). Edge denials never reach core, so — unlike the retired in-tree
  transport — they record no `governance_audit_events` row at all; the
  matrix asserts logs and challenge shape because those are what this
  browser-driven e2e can observe without querying Postgres. Denial-side audit
  parity is tracked in eshu-hq/eshu#5567. The
  revoked-token matrix row is intentionally omitted (it is the identity-token
  resolver's path, not oidcbearer, and is owner-self-scoped — only the
  post-`require_sso`-flip-dead wizard session could revoke it).
- **Discovery is static operator config, not DB state (eshu-hq/eshu#9731).**
  The satellite serves the RFC 9728 document from boot whenever the stack
  configures issuers (`ESHU_AUTH_AUTHORIZATION_SERVERS`), so shapes A and C
  assert a static 200 (not a 404), credential-less 401s steer to discovery
  (not bare `Bearer`), shape B asserts the document instead of polling for a
  live flip, and `eshu mcp setup --hosted` auto-resolves SSO posture whenever
  the edge advertises issuers. Likewise, satellite-forwarded reads emit no
  `scoped_read_allowed` audit row (that sink lived only in the retired in-tree
  MCP transport middleware), so shape A asserts the count stays flat. Step
  ids are unchanged for the manifest gate; the asserted contracts moved.

## Local-only, not in CI

The 5× flake loop and the source-mutation variant of the sensitivity proof stay
local. A one-time manual remote-rig run is the acceptance evidence for the
remote validation surface; the suite is otherwise credential-free by
construction (mock IdP + stubbed GitHub, no cloud accounts, no secrets).
