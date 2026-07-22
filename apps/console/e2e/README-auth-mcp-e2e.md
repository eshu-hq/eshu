# MCP-identity auth E2E suite (F-9, #5170)

A sibling of the #4971 browser-auth suite (`runAuthE2E.ts`), scoped to the
**MCP HTTP transport** identity story of epic #5161: it proves auth follows the
configured identity story on `GET /sse`, `POST /mcp/message`, and
`/.well-known/oauth-protected-resource` across three org shapes, plus a
negative-leakage module.

Run it (owns the `docker-compose.e2e.yaml` stack lifecycle on an isolated
project + 29xxx ports):

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
| `authMcpE2EShapeB.ts` | Shape B (OIDC via the mock IdP): provider CRUD, discovery-flip poll, precedence regression, `require_sso` flip. |
| `authMcpE2EOauthClient.ts` | Pure-`fetch` scripted RFC 9728 + RFC 7636 (PKCE) OAuth client — MCP OAuth is a machine flow, no browser. Includes the in-network→localhost URL rewrite the browser gets via Chromium `--host-resolver-rules`. |
| `authMcpE2ELeakage.ts` + `authMcpE2ELeakageDenials.ts` | Negative-leakage module: credential-less probes, the distinct bad-credential denial matrix, the non-vacuous cross-scope row filter, and the raw-token-absence scan. |
| `authMcpE2EGraphSeed.ts` | Seeds one `Repository` node into NornicDB over its Neo4j-compatible HTTP endpoint, and parses the resolver's denial-outcome log lines. |
| `authMcpE2EJsonRpc.ts` / `authMcpE2EPsql.ts` | Shared JSON-RPC-over-HTTP client and psql helpers. |

Verifiers: `scripts/verify-auth-mcp-e2e-manifest.sh` (report vs
`testdata/golden/auth-mcp-e2e-baseline.json`) and
`scripts/verify-auth-mcp-e2e-sensitivity.sh` (mutation-sensitivity of the
negative module). Both run in the `auth-mcp-e2e` CI job
(`.github/workflows/frontend.yml`, registered in `specs/ci-gates.v1.yaml`).

## Two adaptations worth knowing

- **Row-filter dimension is scope grant, not tenant.** `list_indexed_repositories`
  is graph-backed here, and repositories carry no `tenant_id` (only `id`/`name`).
  The non-vacuous row-filter proof seeds one graph node and shows it is visible
  to an AllScopes cookie session but filtered to zero for a scoped (empty-grant)
  bearer. No MCP *bearer* credential in this stack is AllScopes — personal
  identity tokens and OIDC bearers both resolve to `AuthModeScoped` with an
  empty grant — so the AllScopes reader is shape B's surviving SSO-admin cookie
  session.
- **Denial distinctness comes from resolver logs + challenge shape.** Denial-side
  `governance_audit_events` reason codes do not exist yet, so the matrix asserts
  the oidcbearer resolver's structured-log `outcome` (`unknown_issuer` /
  `wrong_audience` / `malformed`) and the RFC 9728 challenge shape (pre-match →
  `resource_metadata`, post-match → bare `Bearer`). A first-class denial-reason
  in the audit table is tracked at
  [eshu-hq/eshu#5567](https://github.com/eshu-hq/eshu/issues/5567). The
  revoked-token matrix row is intentionally omitted (it is the identity-token
  resolver's path, not oidcbearer, and is owner-self-scoped — only the
  post-`require_sso`-flip-dead wizard session could revoke it).

## Local-only, not in CI

The 5× flake loop and the source-mutation variant of the sensitivity proof stay
local. A one-time manual remote-rig run is the acceptance evidence for the
remote validation surface; the suite is otherwise credential-free by
construction (mock IdP + stubbed GitHub, no cloud accounts, no secrets).
