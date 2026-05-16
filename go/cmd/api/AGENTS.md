# AGENTS.md — cmd/api guidance for LLM assistants

## Read first

1. `go/cmd/api/wiring.go` — the full wiring sequence: how Postgres and the
   graph driver are opened, how the query router is assembled, and how the
   admin surface is mounted via `mountRuntimeSurface`.
2. `go/cmd/api/main.go` — `main`, telemetry bootstrap, signal handling, and
   `http.Server` configuration.
3. `go/internal/query/handler.go` — `APIRouter` and `APIRouter.Mount`; understand
   this before adding handler families or changing route registration.
4. `go/internal/runtime/` — `NewStatusAdminMux`, `OpenNeo4jDriver`,
   `ResolveAPIKey`; the shared admin seams this binary delegates to.
5. `go/cmd/api/README.md` — lifecycle summary, env vars, and operational notes.

## Invariants this package enforces

- **Read-only runtime** — `cmd/api` opens no write paths. All wiring flows into
  read adapters (`query.Neo4jReader`, `query.ContentReader`). The binary never
  calls any graph write method or Postgres write method. Enforced structurally:
  handler structs hold read-port interfaces.
- **Required Postgres DSN** — `wireAPI` returns an error after `ResolveAPIKey`
  succeeds if both `ESHU_POSTGRES_DSN` and `ESHU_CONTENT_STORE_DSN` are empty;
  the binary exits at startup (`wiring.go:42`).
- **Profile and backend validated at startup** — `loadQueryProfile` and
  `loadGraphBackend` call `ParseQueryProfile` and `ParseGraphBackend` respectively;
  unrecognized values return errors that cause `os.Exit(1)` (`wiring.go:147`).
- **Auth wraps the full mux** — `AuthMiddleware` is applied after
  `mountRuntimeSurface`, so data routes cannot be reached without auth when a
  token is configured (`wiring.go:105`).
- **Graceful shutdown bounded at 5 s** — the shutdown goroutine calls
  `Shutdown` on the server with a 5-second context; requests not completed within
  that window are interrupted (`main.go:93`).
- **Compile-time port conformance** — `wiring.go:23` asserts that `Neo4jReader`
  satisfies `GraphQuery` and `ContentReader` satisfies `ContentStore`; removing
  these assertions will silently break port conformance.

## Common changes and how to scope them

- **Add a new handler family** → add the handler struct to `internal/query`,
  add a `Mount` call in `APIRouter.Mount`, wire the struct in `newRouter`
  (`wiring.go:163`), add an `openapi_paths_*.go` fragment inside `internal/query`
  and reference it in the OpenAPI assembly function, update
  `docs/docs/reference/http-api.md`, run
  `go test ./cmd/api ./internal/query -count=1`. Why: all handler families
  follow the same struct-and-`Mount` pattern; missing a step leaves routes
  unreachable or undocumented.

- **Change the listen address or timeouts** → edit `main.go:74` for
  `ESHU_API_ADDR` and `main.go:83` for the `http.Server` timeout fields. Timeouts
  are hard-coded constants; there are no env vars for them. Why: timeout values
  are deployment-level knobs that should be changed deliberately, not silently
  picked up from environment.

- **Swap the graph backend** → set `ESHU_GRAPH_BACKEND=nornicdb` or
  `ESHU_GRAPH_BACKEND=neo4j`; the binary delegates to `ParseGraphBackend` and
  `openQueryGraph`. Do not add backend-conditional branches in this package;
  those belong in `internal/storage/cypher` adapters.

- **Add a new environment variable** → read it in `wireAPI` via the `getenv`
  function parameter (not `os.Getenv` directly), update `doc.go` and `README.md`,
  and add it to `docs/docs/reference/cli-reference.md`. Why: `wireAPI` takes
  `getenv func(string) string` so tests can inject values without `t.Setenv`.

## Failure modes and how to debug

- Symptom: binary exits immediately after `eshu-api` starts → likely cause:
  missing `ESHU_POSTGRES_DSN`, bad `ESHU_QUERY_PROFILE`, or graph driver
  unreachable → check the structured log `runtime.startup.failed` event; it
  carries the specific error.

- Symptom: `/healthz` returns 503 → likely cause: `NewStatusAdminMux` or the
  status reader returned an error → check `/admin/status` for the reported stage
  and failure fields.

- Symptom: high latency on data routes → likely cause: slow graph or Postgres
  queries → check `eshu_dp_neo4j_query_duration_seconds` and
  `eshu_dp_postgres_query_duration_seconds` at `/metrics`; trace individual
  requests via the `otelhttp` span named `eshu-api`.

- Symptom: 401 on all data routes → likely cause: bearer token mismatch → the
  `AuthMiddleware` in `internal/query` skips auth only when the resolved token is
  empty (dev mode); verify the token resolves via `ResolveAPIKey` in
  `internal/runtime`.

- Symptom: requests interrupted mid-stream during redeploy → likely cause: the
  5-second graceful `Shutdown` window was exceeded → consider a query-side timeout
  on long graph traversals before extending the server shutdown window
  (`main.go:93`).

## Anti-patterns specific to this package

- **Calling `os.Getenv` directly inside `wireAPI`** — `wireAPI` takes
  `getenv func(string) string` for test injection. Direct `os.Getenv` calls inside
  `wireAPI` make the wiring untestable without `t.Setenv`.

- **Adding backend-conditional branches to `wiring.go`** — backend brand
  differences belong in `internal/storage/cypher` adapters behind the `GraphQuery`
  port. Adding `if graphBackend == "nornicdb"` in this package leaks dialect logic
  into the transport layer.

- **Adding data routes directly to `apiMux` in `wireAPI`** — new routes belong in
  handler structs under `internal/query` with a `Mount` method. Bypassing the
  `APIRouter` means the route misses OpenAPI registration, capability-matrix
  gating, and the standard response-envelope contract.

- **Mounting the admin surface after `AuthMiddleware`** — admin endpoints
  (`/healthz`, `/readyz`, `/admin/status`, `/metrics`) must remain public.
  `mountRuntimeSurface` is called before `AuthMiddleware` is applied; reversing
  that order blocks health probes.

## What NOT to change without an ADR

- `ESHU_QUERY_PROFILE` accepted values — part of the public truth-label contract;
  see `docs/docs/reference/http-api.md` and `go/internal/query/contract.go`.
- `ESHU_GRAPH_BACKEND` accepted values — governed by the backend promotion gate;
  see `docs/docs/adrs/2026-04-22-nornicdb-graph-backend-candidate.md`.
- The `AuthMiddleware` placement relative to `mountRuntimeSurface` — moving this
  changes which routes require auth; that is a security-boundary change.
