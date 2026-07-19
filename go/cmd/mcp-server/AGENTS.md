# AGENTS.md — cmd/mcp-server guidance for LLM assistants

## Read first

1. `go/cmd/mcp-server/README.md` — pipeline position, lifecycle, configuration,
   and operational notes
2. `go/cmd/mcp-server/wiring.go` — `wireAPI` (env-var wiring, credential chain,
   `mcpAuthWiring`); `go/cmd/mcp-server/wiring_router.go` — `newMCPQueryRouter`
   and `newMCPQueryRouterWithSemanticEmbedding` (handler composition);
   `go/cmd/mcp-server/transport_auth_guard.go` — the no-silent-open startup gate
   (#5168). Understand these before touching handler composition, env-var
   wiring, or transport auth
3. `go/cmd/mcp-server/main.go` — transport selection and shutdown; understand
   the `switch transport` before touching startup or signal handling
4. `go/internal/mcp/README.md` — MCP tool dispatch, the SSE session model, and
   the protocol handler
5. `go/internal/telemetry/instruments.go` and `contract.go` — metric and span
   names before adding new telemetry

## Invariants this package enforces

- **Validation before datastore** — `wireAPI` resolves `loadQueryProfile`,
  `loadGraphBackend`, and `ResolveAPIKey` before opening any connection
  (`wiring.go:32`). An invalid profile, backend, or key returns an error before
  any dial.
- **Postgres required** — `wireAPI` returns an error if both `ESHU_POSTGRES_DSN`
  and `ESHU_CONTENT_STORE_DSN` are empty. The `openQueryGraph` call is skipped
  when `ProfileLocalLightweight` is active or `ESHU_DISABLE_NEO4J` is true
  (`wiring.go:179`).
- **IaC stores always wired** — `newMCPQueryRouterWithSemanticEmbedding`
  (`wiring_router.go`) always sets `IaCHandler.Reachability` and
  `IaCHandler.Management` to Postgres-backed query adapters. Do not set either
  to nil.
- **No silent open mode over HTTP (#5168)** — in `http` mode with no resolvable
  credential source (`ESHU_API_KEY`, `ESHU_SCOPED_TOKENS_FILE`, or
  `ESHU_AUTH_RESOURCE_URI`), `requireMCPHTTPCredentialSource` exits non-zero
  unless `ESHU_MCP_ALLOW_UNAUTHENTICATED=true`. Do not weaken this by counting
  the always-wired Postgres identity resolver as a credential source. stdio is
  never gated.
- **MCP read tools must have matching query handlers** —
  `newMCPQueryRouterWithSemanticEmbedding` (`wiring_router.go`) wires
  `CICDHandler` and `SupplyChainHandler` to their Postgres read models so
  `list_ci_cd_run_correlations`, `list_supply_chain_impact_findings`,
  `list_security_alert_reconciliations`, and
  `list_sbom_attestation_attachments` do not dispatch to 404 routes.
- **Auth on query routes** — `query.AuthMiddleware` wraps the `query.APIRouter`
  handler before it is passed to `mcp.NewServer`. The MCP transport endpoints
  (`/sse`, `/mcp/message`, `/health`) handle auth separately inside the MCP
  transport mux.
- **stdio mode has no HTTP admin surface** — the admin mux is passed to
  `NewServer` only in HTTP mode. In `stdio` mode the transport switch at
  `main.go:54` does not call `NewServer` with an admin mux, so those routes
  are not mounted.
- **Telemetry shutdown on background context** — `telemetry.NewProviders`
  (`main.go:37`) returns a providers value whose `Shutdown` is called with
  `context.Background()`, not with the cancelled root context, so traces
  flushed during shutdown complete.

## Common changes and how to scope them

- **Add a new query handler** → add a field to the `query.APIRouter` struct,
  wire it in `newMCPQueryRouterWithSemanticEmbedding` in `wiring_router.go`,
  assert it in
  `wiring_test.go`, and add the matching tool in `go/internal/mcp/dispatch.go`.
  Run
  `cd go && go test ./cmd/mcp-server ./internal/mcp -count=1`. Why: the
  compile-time assertions (`query.Neo4jReader` satisfies `query.GraphQuery`,
  `query.ContentReader` satisfies `query.ContentStore` — `wiring.go:22`) fail
  if the handler does not
  satisfy its interface; the dispatch route test in `internal/mcp` fails if
  the route is missing.

- **Change transport default** → edit the fallback in `main.go:40` and update
  `doc.go`. Run `go test ./cmd/mcp-server -count=1`. Why: `doc.go` documents
  the default and is read by the service description surface.

- **Add a new env var** → read it via `getenv` in `wireAPI` or `main`, add it
  to the configuration table in `README.md`, and add a test in `wiring_test.go`
  that asserts failure before datastore connection when the var is invalid. Why:
  all env validation must complete before datastore connections.

- **Change the admin surface** → touch `mountRuntimeSurface` in `wiring.go` and
  update the corresponding test in `runtime_surface_test.go`. Why: the tests
  assert `/healthz`, `/readyz`, `/metrics`, and `/admin/status` routes are
  present and return correct shapes.

## Failure modes and how to debug

- Symptom: binary exits 1 immediately on startup → check structured log for
  `event_name=runtime.startup.failed`; sub-causes are bad API key, bad profile,
  bad backend, Postgres dial failure, or telemetry init failure.

- Symptom: MCP client receives no tools → the server started in `stdio` mode
  but the client is pointing at an HTTP URL, or vice versa; check
  `ESHU_MCP_TRANSPORT`.

- Symptom: `/healthz` returns 404 in stdio mode → by design; admin routes are
  only mounted in HTTP mode via `Server.RunHTTP`.

- Symptom: MCP tool returns auth error on `/api/v0/*` routes → API key missing
  or wrong; `query.AuthMiddleware` enforces it on all `/api/` routes.

- Symptom: `ESHU_POSTGRES_DSN` set but Postgres ping fails → check Postgres
  reachability and credentials; `wireAPI` will return before Neo4j dial.

## Anti-patterns specific to this package

- **Calling handler methods directly** — do not call `query.RepositoryHandler`
  or other handlers from `wiring.go` outside of `query.APIRouter.Mount`. All
  routing goes through the `APIRouter`.

- **Setting `ESHU_DISABLE_NEO4J` in production** — this skips the Neo4j dial
  and limits query capability. It is intended for lightweight local profiles
  only.

- **Confusing the two auth layers** — as of #5168 the MCP transport endpoints
  (`GET /sse`, `POST /mcp/message`) run through the credential middleware, via
  `mcp.WithTransportAuth` wired in `wireAPI`. The `/api/*` routes are protected
  separately by `query.AuthMiddlewareWithScopedTokensAndGovernanceAudit`
  wrapping the query mux (the `authedHandler` passed to `mcp.NewServer`). Both
  use the SAME credential chain; do not assume one covers the other's mount, and
  do not remove either wrap. Note the residual: a headerless request is refused
  only when a shared `ESHU_API_KEY` is set — a scoped-only/OIDC-only deployment
  still passes headerless requests through the shared-token dev-bypass until the
  companion auth-headerless-bypass hardening (under #5161) lands.

## What NOT to change without an ADR

- The query handler composition in `newMCPQueryRouterWithSemanticEmbedding`
  (`wiring_router.go`) — adding or removing handlers changes the MCP tool
  surface and must be coordinated with `internal/mcp/dispatch.go` tool
  definitions and `docs/public/guides/mcp-guide.md`.
- Transport options for `ESHU_MCP_TRANSPORT` — adding a new transport type
  changes the documented wire contract; see `docs/public/deployment/service-runtimes.md`.
