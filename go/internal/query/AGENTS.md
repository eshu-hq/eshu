# AGENTS.md — internal/query guidance for LLM assistants

## Read first

1. `go/internal/query/contract.go` — `QueryProfile`, `GraphBackend`,
   `TruthLevel`, `TruthBasis`, `capabilityMatrix`, `BuildTruthEnvelope`, and the
   profile-gate helpers; every handler that returns truth metadata must understand
   this file.
2. `go/internal/query/handler.go` — `APIRouter`, `APIRouter.Mount`, and the four
   response-writing helpers (`WriteJSON`, `WriteError`, `WriteSuccess`,
   `WriteContractError`); these are the shared conventions every handler uses.
3. `go/internal/query/ports.go` — `GraphQuery` and `ContentStore` interface
   definitions; understand the contract before touching any handler that reads
   from the graph or content store.
4. `go/internal/query/openapi.go` and the `openapi_paths_*.go` files — how the
   OpenAPI spec is assembled; any new or changed route must update the matching
   fragment.
5. `go/internal/telemetry/contract.go` — span name constants
   (`SpanQueryRelationshipEvidence`, `SpanQueryDeadIaC`,
   `SpanQueryIaCUnmanagedResources`, `SpanQueryInfraResourceSearch`) and log
   key conventions; check here before adding new telemetry.

## Invariants this package enforces

- **Capability gate before any read** — handlers call the unexported
  `capabilityUnsupported` helper before touching `GraphQuery` or `ContentStore`.
  A nil max-truth means the capability is blocked at the current profile.
  `capabilityUnsupported` consults the `capabilityMatrix` map in `contract.go:127`
  which stores `TruthLevelExact` and `TruthLevelDerived` ceiling values per
  profile. On failure, handlers call `WriteContractError` (`handler.go:40`).

- **`BuildTruthEnvelope` panics on unknown capability** — every capability string
  passed to `BuildTruthEnvelope` must exist in `capabilityMatrix`
  (`contract.go:432`). Add the capability to the map before the handler is
  callable.

- **Port boundary** — no handler calls `neo4jdriver.DriverWithContext` or
  `*sql.DB` directly. All graph reads go through `GraphQuery`, content reads go
  through `ContentStore`, and reducer fact reads go through query-local store
  ports such as `IaCManagementStore`. Concrete adapters (`Neo4jReader`,
  `ContentReader`, `PostgresIaCManagementStore`) are the only query types that
  touch drivers. Enforced structurally: handler structs hold interface fields,
  not concrete types.

- **Envelope negotiation is stable** — `WriteSuccess` branches on
  `acceptsEnvelope(r)` (`handler.go:29`). MCP tool dispatch relies on the
  `ResponseEnvelope` shape when `Accept: application/eshu.envelope+json` is sent.
  Do not change the envelope field names or remove the negotiation branch.

- **OpenAPI fragments and handler behavior must agree** — the spec is a
  concatenation of string literals in `openapi_paths_*.go` files. A handler
  change that adds a field or changes a route must update the matching fragment
  in the same PR, or the live spec diverges from actual behavior.

- **Dead-code scans de-duplicate entity IDs across candidate labels** —
  `scanDeadCodeCandidates` applies `filterDuplicateDeadCodeRows`
  (`code_dead_code_scan.go:107`) before hydration. Keep this when adding a
  candidate label such as SQL functions, or multi-label graph rows can inflate
  results, content reads, and candidate row counts.

- **Use the dead-code `language` filter for language maturity proof** —
  `deadCodeCandidateLabelsForLanguage` narrows SQL scans to `SqlFunction`
  (`code_dead_code_scan.go:72`) so mixed repositories cannot fill the page
  before SQL routine evidence is evaluated. Perl and other source-language
  slices also rely on the filter during dogfood so earlier candidate labels do
  not hide language-specific evidence. Keep this path when adding or dogfooding
  a language-specific dead-code slice.

- **SQL routine reachability uses graph `EXECUTES` probes** —
  `CodeHandler.filterDeadCodeResultsWithoutIncomingEdges` falls through to
  `deadCodeResultsWithGraphIncomingEdges` for `SqlFunction` candidates
  (`code_dead_code_scan.go:128`, `code_dead_code_scan.go:240`) because SQL
  relationship materialization graph-writes `EXECUTES` edges directly instead
  of storing completed shared-projection intent rows. Keep the probe batched;
  reverting to one graph call per SQL routine can make large dead-code pages
  too expensive, while removing the fallback can report trigger-bound SQL
  routines as cleanup candidates.

- **`Neo4jReader` opens one session per query** — `Run` and `RunSingle` open and
  close a session within the call. Do not hold or share sessions across handler
  calls (`neo4j.go:50`).

## Common changes and how to scope them

- **Add a new HTTP handler** → create a handler struct with `Neo4j GraphQuery`
  and/or `Content ContentStore` fields, add a `Mount(mux *http.ServeMux)` method
  with explicit `mux.HandleFunc` calls, add the struct field to `APIRouter`
  (`handler.go:110`), call `Mount` in `APIRouter.Mount` (`handler.go:125`), wire
  the concrete adapter in `cmd/api/wiring.go`'s `newRouter`, add a
  `openapi_paths_*.go` fragment and reference it in `OpenAPISpec()`, update
  `docs/docs/reference/http-api.md`. Run
  `go test ./cmd/api ./internal/query -count=1`. Why: missing any step leaves a
  route reachable but not documented, not gated, or not wired to the right
  adapter.

- **Add a new capability** → add an entry to `capabilityMatrix` in `contract.go`
  with per-profile max truth levels; add the capability ID constant near the
  existing `const` blocks if reused across handlers; call `BuildTruthEnvelope`
  with the new ID in the handler; update `specs/capability-matrix.v1.yaml` and
  `docs/docs/reference/http-api.md`. Run `go test ./internal/query -count=1`
  (the `contract_endpoint_test.go` validates matrix coverage). Why:
  `BuildTruthEnvelope` panics on unknown capability IDs at handler call time.

- **Change a response shape** → update the handler method, the matching
  `openapi_paths_*.go` string constant, and `docs/docs/reference/http-api.md` in
  the same PR. Why: the OpenAPI spec is a static string; it does not reflect from
  Go structs automatically.

- **Add a new graph query** → write the Cypher in the handler or a helper file
  named after the domain (`repository_*.go`, `code_*.go`); call
  `Neo4jReader.Run` or `RunSingle`; use `StringVal`, `BoolVal`, `IntVal` to
  extract row values; add an OTEL span via `startQueryHandlerSpan` if the query
  represents a distinct user-visible capability. Why: consistent span attributes
  (`http.route`, `eshu.capability`) let operators correlate latency metrics to
  specific capabilities.

## Failure modes and how to debug

- Symptom: HTTP 501 with `error.code=unsupported_capability` → likely cause:
  the current `QueryProfile` does not support the capability → check
  `truth.profiles.required` in the response; verify the ESHU_QUERY_PROFILE env
  var in the running API process.

- Symptom: `repository_query.stage_completed` log events show one stage
  dominating → likely cause: slow graph or Postgres query at that stage → inspect
  `eshu_dp_neo4j_query_duration_seconds` labeled by the Cypher statement, or
  `eshu_dp_postgres_query_duration_seconds` for content reads.

- Symptom: span `query.relationship_evidence` shows high latency → likely cause:
  slow Postgres relationship evidence read model query → check `ContentReader`
  Postgres span labeled `db.operation=get_relationship_evidence` and the
  underlying `resolved_relationships` table.

- Symptom: panic in production with `query capability ... missing from capability
  matrix` → a new handler called `BuildTruthEnvelope` with an unregistered
  capability → add the missing entry to `capabilityMatrix` in `contract.go:127`
  and the matching YAML spec.

- Symptom: MCP tool calls receive unexpected payload shape (missing `data`
  wrapper) → likely cause: handler used `WriteJSON` instead of `WriteSuccess`, or
  the client is not sending `Accept: application/eshu.envelope+json` → confirm the
  MCP transport sets the correct `Accept` header; confirm the handler calls
  `WriteSuccess`.

## Anti-patterns specific to this package

- **Branching on `GraphBackend` in handler code** — backend-specific Cypher
  differences (NornicDB vs Neo4j) belong in `internal/storage/cypher` adapters,
  not in handler methods. Exception: `CodeHandler.graphBackend()` routes to
  NornicDB-specific relationship helpers (`code_relationships_nornicdb.go`) —
  that is the documented narrow seam.

- **Directly importing `neo4jdriver` in handler files** — handler structs hold
  `GraphQuery`, not `neo4jdriver.DriverWithContext`. Only `neo4j.go` and
  `wiring.go` should import the Neo4j driver.

- **Adding public routes to `publicHTTPPaths` without review** — the map in
  `auth.go:10` bypasses bearer-token auth. Adding a data route here exposes it
  without authentication.

- **Using `panic` for profile-gate failures** — use `WriteContractError` with
  `ErrorCodeUnsupportedCapability` and the structured `ErrorProfiles` fields.
  Panics are reserved for programmer errors like missing capability matrix entries.

## What NOT to change without an ADR

- `capabilityMatrix` entry `RequiredProfile` values — these gate which runtime
  profiles can answer which queries; changes affect CLI, MCP, and HTTP clients
  simultaneously; see `docs/docs/reference/http-api.md` and
  `specs/capability-matrix.v1.yaml`.
- `ResponseEnvelope` and `TruthEnvelope` field names — these are stable wire
  contracts used by MCP tool dispatch and CLI `--json` mode; see
  `docs/docs/reference/http-api.md`.
- `EnvelopeMIMEType` (`application/eshu.envelope+json`) — changing this MIME type
  breaks every client that has already adopted envelope negotiation.
