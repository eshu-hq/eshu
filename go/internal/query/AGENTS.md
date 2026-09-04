# AGENTS.md — internal/query guidance for LLM assistants

## Read first

1. `go/internal/query/querycontract/` — profiles, envelopes, capability
   registration, HTTP helpers, read ports, and their content-model closure.
2. `go/internal/query/contract.go`, `handler.go`, and `ports.go` — compatibility
   aliases and wrappers plus `APIRouter`; existing callers keep the root API.
3. `go/internal/query/querycontract/capability.go` and the root `contract_*`
   files — family registration and the canonical 139-capability order.
4. `go/internal/query/openapi.go` and the `openapi_paths_*.go` files — how the
   OpenAPI spec is assembled; any new or changed route must update the matching
   fragment.
5. `go/internal/telemetry/contract.go` — span name constants
   (`SpanQueryRelationshipEvidence`, `SpanQueryDeadIaC`,
   `SpanQueryIaCUnmanagedResources`, `SpanQueryInfraResourceSearch`,
   `SpanQueryCodeStructuralInventory`, `SpanQueryCodeTopicInvestigation`,
   `SpanQueryDeadCodeInvestigation`) and log key conventions; check here
   before adding new telemetry.

## Invariants this package enforces

- **Capability gate before any read** — handlers call the unexported
  `capabilityUnsupported` helper before touching `GraphQuery` or
  `ContentStore`. A nil max-truth means the capability is blocked at the
  current profile. The helper delegates to querycontract's registry, whose live
  compatibility view remains `capabilityMatrix`. On failure, handlers call
  `WriteContractError`.

- **`BuildTruthEnvelope` panics on unknown capability** — every capability
  string passed to `BuildTruthEnvelope` must be registered through
  querycontract or the compatibility matrix before the handler is callable.

- **Port boundary** — no handler calls `neo4jdriver.DriverWithContext` or
  `*sql.DB` directly. All graph reads go through `GraphQuery`, content reads go
  through `ContentStore`, and reducer fact reads go through query-local store
  ports such as `IaCManagementStore`. Concrete adapters (`Neo4jReader`,
  `ContentReader`, `PostgresIaCManagementStore`) are the only query types that
  touch drivers. Enforced structurally: handler structs hold interface fields,
  not concrete types.

- **Envelope negotiation is stable** — `WriteSuccess` branches on
  `acceptsEnvelope(r)` (`handler.go:29`). MCP tool dispatch relies on the
  `ResponseEnvelope` shape when `Accept: application/eshu.envelope+json` is
  sent. Do not change the envelope field names or remove the negotiation
  branch.

- **OpenAPI fragments and handler behavior must agree** — the spec is a
  concatenation of string literals in `openapi_paths_*.go` files. A handler
  change that adds a field or changes a route must update the matching fragment
  in the same PR, or the live spec diverges from actual behavior.

- **Repository tenant-isolation canary evidence** — #2048 filters repository
  list and selector reads from `AuthContext` before pagination, counts,
  ambiguity, and not-found decisions.

- **Code search scoped-token route evidence** — #2062 opens only `POST
  /api/v0/code/search` after `CodeHandler` applies `AuthContext` bounds to
  repository selector ambiguity, graph search predicates, and content fallback
  calls. Scoped graph search adds the repository/scope-id predicate before
  `LIMIT`; scoped content fallback queries authorized repositories individually
  and never calls all-repository content methods.

- **Entity resolution scoped-token route evidence** — #2064 opens only `POST
  /api/v0/entities/resolve` after `EntityHandler` applies `AuthContext` bounds
  to selector ambiguity, graph entity predicates, repo-identity hydration, and
  content fallback calls. Scoped graph resolution adds the repository/scope-id
  predicate before `LIMIT`; scoped content fallback queries authorized
  repositories individually and never calls all-repository content methods.

- **Content scoped-token route evidence** — #2066 opens only the content
  file/entity read and search routes after `ContentHandler` applies
  `AuthContext` bounds to repository selector resolution, no-repo search scope,
  exact entity read repo checks, and empty-grant short-circuits. Scoped search
  uses authorized repository IDs before result counting and truncation; scoped
  exact reads return not found for out-of-grant rows without returning payload
  fields.

- **Evidence citation scoped-token route evidence** — #2068 opens only `POST
  /api/v0/evidence/citations` after `EvidenceHandler` applies `AuthContext`
  bounds to file-handle hydration and entity-result filtering. Empty scoped
  grants return zero resolved citations without content-store hydration;
  out-of-scope file handles are never sent to the file batch reader, and
  out-of-scope entity rows are treated as missing before citation payloads are
  built.

- **Entity context scoped-token route evidence** — #2070 opens only `GET
  /api/v0/entities/{entity_id}/context` after `EntityHandler` applies
  `AuthContext` bounds to empty grants, graph entity predicates, repo-identity
  hydration, and content fallback rows. Scoped graph context reads add an
  authorized repository predicate before relationship hydration; scoped content
  fallback treats out-of-grant entity rows as not found before reading
  relationships.

- **Service/workload context scoped-token route evidence** — #2072 opens only
  `GET /api/v0/workloads/{workload_id}/context`, `GET
  /api/v0/workloads/{workload_id}/story`, `GET
  /api/v0/services/{service_name}/context`, and `GET
  /api/v0/services/{service_name}/story` after `EntityHandler` applies
  `AuthContext` bounds to empty grants, workload lookup predicates, service
  candidate selection, repository selector disambiguation, and read-model
  fallback rows.

- **Service investigation scoped-token route evidence** — #2074 opens only `GET
  /api/v0/investigations/services/{service_name}` after `EntityHandler` applies
  `AuthContext` bounds to empty grants, service candidate selection, repository
  selector disambiguation, environment filtering, read-model fallback rows,
  coverage metadata, and recommended next calls. MCP dispatch for
  `investigate_service` remains transport-only and forwards service,
  repository, and environment selectors through the shared HTTP handler.

- **Query playbook scoped-token route evidence** — #2076 opens only `GET
  /api/v0/query-playbooks` and `POST /api/v0/query-playbooks/resolve` because
  `QueryPlaybookHandler` reads deterministic in-process catalog data and never
  calls graph, Postgres, providers, collectors, or tenant data stores.
  Live-data route targets referenced by resolved playbook steps remain governed
  by their own scoped-route allowlist entries.

- **Vulnerability scanner contract scoped-token route evidence** — #2078 opens
  only `GET /api/v0/supply-chain/vulnerability-scanner/contract` because
  `SupplyChainHandler.getVulnerabilityScannerReadContract` returns a
  deterministic in-process route/filter contract and never calls graph,
  Postgres, providers, collectors, repositories, tenants, or token stores. Live
  scanner findings, counts, inventories, explanations, and provider-alert
  routes stay governed by their own scoped-route allowlist entries.

- **Hosted governance status scoped-token route evidence** — #2080 opens only
  `GET /api/v0/status/governance` because `StatusHandler.getGovernanceStatus`
  returns redacted runtime governance posture: normalized modes, safe revision
  hashes, booleans, and aggregate counts. Existing governance status tests
  prove policy bodies, private source IDs, credential handles, raw provider
  details, prompts, provider responses, private endpoint-like values, and local
  paths are not returned. The route does not read graph, content, repositories,
  supply-chain findings, provider payloads, collectors, raw tenants, raw
  workspaces, or token values.

- **Semantic extraction status scoped-token route evidence** — #2082 opens only
  `GET /api/v0/status/semantic-extraction` because
  `StatusHandler.getSemanticExtractionStatus` returns redacted runtime semantic
  extraction posture: provider availability state, source-class enablement,
  deterministic-path impact, supported enum values, aggregate queue counts,
  budget counters, and audit class counts. Provider profile detail text stays
  out of the response; raw prompts, provider responses, credential handles,
  token values, private endpoints, tenant/workspace ids, repository/source ids,
  graph reads, content reads, and provider payloads remain outside the status
  route.

- **Component extension scoped-token route evidence** — #2084 opens only `GET
  /api/v0/component-extensions` and `GET
  /api/v0/component-extensions/{component_id}/diagnostics` because
  `ComponentExtensionsHandler` returns bounded local component registry
  posture: package ids, names, publishers, versions, manifest digests,
  lifecycle states, activation config handles, trust-policy booleans, and
  stable policy/error codes. Local manifest paths, activation config paths, raw
  component config, registry file paths, credentials, endpoints,
  tenant/workspace ids, repository ids, graph reads, content reads, and
  provider payloads remain outside the response.

- **Hosted readiness scoped-token route evidence** — #2090 opens only `GET
  /api/v0/status/hosted-readiness` because `StatusHandler` returns bounded
  hosted readiness checks, queue counters, repository count, diagnostic route
  names, and aggregate coordinator counters. Scoped responses replace
  coordinator instance rows with `scopedCoordinatorToMap`, so collector
  instance ids, display names, tenant/workspace values, queue conflict keys,
  repository/source ids, graph row detail, provider payloads, local paths, and
  credentials stay outside the payload.

- **Collector status scoped-token route evidence** — #2088 opens only `GET
  /api/v0/status/collectors` because `StatusHandler.listCollectors` returns
  aggregate runtime posture for scoped tokens: collector kind,
  runtime/category/health buckets, collector counts, coordinator/enabled/
  bootstrap/claim counts, evidence-source summaries, observation counts, and
  aggregate timestamps. Scoped responses do not expose collector instance ids,
  display names, source systems, detail text, tenant/workspace values, queue
  conflict keys, repository/source ids, graph reads, content reads,
  credentials, endpoints, local paths, or provider payloads. The legacy
  `/api/v0/collectors` route remains fail-closed for scoped tokens.

- **Ingester status scoped-token route evidence** — #2086 opens only `GET
  /api/v0/status/ingesters` and `GET /api/v0/status/ingesters/{ingester}`
  because `StatusHandler` returns bounded runtime health, queue,
  scope-activity, stage-summary, domain-backlog, and coordinator aggregate
  counters for the repository ingester. Scoped-token detail responses replace
  coordinator instance rows with aggregate counts so collector instance ids,
  display names, tenant/workspace values, queue conflict keys,
  repository/source ids, graph reads, content reads, credentials, endpoints,
  local paths, and provider payloads remain outside the payload.

- **Semantic evidence scoped-token route evidence** — #2110 opens only `GET
  /api/v0/semantic/documentation-observations` and `GET
  /api/v0/semantic/code-hints` after `SemanticEvidenceHandler` intersects the
  fact-record read model with `AuthContext` repository/scope grants before
  ordering and paging. Empty grants return empty pages without broad fact
  reads.

- **Package registry reads stay anchored** — `PackageRegistryHandler` in
  `package_registry.go` must require `limit` plus a route-specific anchor
  before graph reads: package lookups use `package_id` or `ecosystem`, version
  lookups use `package_id`, and dependency lookups use `package_id` or
  `version_id`. Do not add whole-graph package scans, and do not present
  package source hints as ownership, publication ownership, or runtime
  consumption truth. Scoped tokens may read only package-registry correlations
  after repository/scope grants are applied before ordering and limits;
  adjacent package identity, version, dependency, count, and inventory routes
  stay fail-closed until each has route-specific proof. Scoped tokens may read
  CI/CD run correlation list/count/inventory routes only after repository/scope
  grants are applied before ordering, limits, grouping, offsets, and
  truncation; README carries the focused no-regression and no-observability
  markers.

- **Vulnerability impact responses always carry readiness** —
  `SupplyChainHandler.listImpactFindings` (`supply_chain.go`) must call
  `BuildSupplyChainImpactReadiness` and emit the envelope on every response.
  The readiness layer derives state from existing source-fact and reducer-fact
  counts only; do not invent findings, do not move reducer matching into the
  readiness path, and do not collapse the six readiness states into one
  severity bucket. The `Readiness` store is optional in tests but required in
  production wiring; a nil store yields `not_configured` so a zero-finding
  answer is never silently treated as `ready_zero_findings`.

- **Dead-code scans de-duplicate entity IDs across candidate labels** —
  `scanDeadCodeCandidates` applies `filterDuplicateDeadCodeRows` (`code_dead_code_scan.go:107`) before
  hydration; keep it when adding a candidate label such as SQL functions, or multi-label rows inflate results, content reads and candidate row counts.

- **Use the dead-code `language` filter for language maturity proof** —
  `deadCodeCandidateLabelsForLanguage` narrows SQL scans to `SqlFunction`
  (`code_dead_code_scan.go:72`) so mixed repositories cannot fill the page
  before SQL routine evidence is evaluated; Perl and other slices rely on it the
  same way. Keep the path when adding or dogfooding a language-specific slice.

- **Keep dead-code investigation conservative for JavaScript/TypeScript** —
  `handleDeadCodeInvestigation` buckets JavaScript, JSX, TypeScript and TSX active
  candidates `ambiguous` until #336 records corpus precision evidence; a missing incoming graph edge alone never promotes them to `cleanup_ready`.

- **The cross-repo hidden-consumer read is a bounded walk** —
  `crossRepoDeadCodeUngrantedConsumerProbeQuery` walks a producer entity's
  distinct consumer repositories in index order, stops at the first outside the
  grant, and returns producer entity ids only. Consumer rows would cost a fan-in
  group per request, a per-repository bound a probe per granted repository; the
  constraints sit at the constant, measured in [#5167 batch 1](../../../docs/internal/evidence/5167-code-family-batch-1.md).

- **A granted consumer outranks a hidden one on every dead-code route** — a
  strong granted edge or consumer settles a candidate reachable/live; a hidden
  consumer forces `ambiguous`/`unknown` with `permission_hidden_consumer` only
  when nothing granted proves use, and `consumer_evidence_truncated` is never
  outranked. `applyDeadCodeIncomingEdges` and `bucketCrossRepoDeadCodeResults`
  both apply it and drifted apart once: change one, change the other.

- **SQL routine reachability uses graph `EXECUTES` probes** —
  `CodeHandler.filterDeadCodeResultsWithoutIncomingEdges` falls through to
  `deadCodeResultsWithGraphIncomingEdges` for `SqlFunction` candidates
  (`code_dead_code_scan.go:128`, `code_dead_code_scan.go:240`) because SQL
  materialization graph-writes `EXECUTES` edges directly, not as completed
  shared-projection intent rows. Keep the probe batched: one call per routine is
  too expensive at page scale, and no fallback calls trigger-bound routines dead.

- **`neo4j_read_policy.go` owns the read session lifecycle** — `Run` and
  `RunSingle` delegate to `runReadAttempts`, which opens and closes a session
  per attempt inside `runReadAttempt`. A single logical read can open up to 2
  sessions (one bounded retry via `maxGraphReadAttempts`). Do not hold or share
  sessions across handler calls.

- **`analyze_infra_relationships` honors `relationship_type` (#3492)** —
  `getRelationships` (`infra_relationship_filter.go`) decodes the optional
  `relationship_type` and resolves it through `resolveInfraRelationshipTypes`'s
  fixed allowlist (semantic MCP aliases `what_deploys` / `what_provisions` /
  `module_consumers` / `who_consumes_xrd` / `what_runs_image` /
  `what_runs_lambda_image` plus canonical edge types such as `DEPLOYS_FROM`,
  `USES_MODULE`, `RUNS_IMAGE`, `AWS_lambda_function_uses_image`). The resolved
  types render into the `OPTIONAL MATCH (n)-[r:TYPE_A|TYPE_B]->(...)` pattern
  as an inline relationship-type filter. An empty argument keeps the prior bare
  untyped pattern (whole-relationship); an unrecognized value returns 400. Do
  not feed free-text into the inline clause — only allowlisted edge-type names
  may render there. Do not add an alias that maps to an edge type the graph
  does not write.

- **`what_deploys` spans the full deployment edge family (#3507)** — the #3492
  alias mapped `what_deploys` to `DEPLOYS_FROM` only, which dropped the runtime
  deployment topology the pre-#3492 untyped read surfaced — notably the
  `WorkloadInstance-[:DEPLOYMENT_SOURCE]->Repository` edge written by
  `canonicalDeploymentSourceUpsertCypher` and read by
  `fetchDeploymentSourcesFromGraph` (`impact_trace_deployment.go`). For a
  workload-instance target the tool could then report an empty deployment
  relationship even when the deployment-source edge exists. `what_deploys` now
  resolves to `{DEPLOYS_FROM, DEPLOYMENT_SOURCE, HAS_DEPLOYMENT_EVIDENCE}` —
  the same deploy family `entity_map_response.go` groups as "deployed by" plus
  the runtime deployment-source edge. When adding a new deployment edge type to
  the graph, add it here too so the deploy alias stays complete.

- **Scope predicate admits the deployment-source topology (#3519)** —
  `infraResourceScopePredicate` (`infra_resource_aggregates.go`) must also
  admit a node whose own `id` is a granted repository (`n.id IN $allowed_*`,
  for the `DEPLOYMENT_SOURCE` Repository neighbor) and a `WorkloadInstance`
  anchored to a granted repo via
  `(:Repository)-[:DEFINES]->(:Workload)<-[:INSTANCE_OF]-(n)` (no USES hop), or
  a `DEPLOYMENT_SOURCE` edge from an in-grant `WorkloadInstance` to a
  `Repository` is dropped even when the repository is in grant. Do not drop the
  label/anchor shape: matching a bare `id` without the `Repository`/`DEFINES`
  anchor would risk authorizing a cross-tenant node by id collision.

- **Scope predicate admits `TerraformStateResource` via `MATCHES_STATE`
  (#5623)** — `infraResourceScopeCoreDisjuncts` admits a
  `TerraformStateResource` only via its `MATCHES_STATE` edge to a granted
  `TerraformResource` (`(alias)<-[:MATCHES_STATE]-(:TerraformResource
  {repo_id:$g})`), never via the node's own `config_repo_id` property. Do not
  switch to a property-only disjunct: `config_repo_id` can be non-null even
  when no `MATCHES_STATE` edge was ever written, so a property-only disjunct
  would wrongly admit an unmatched state resource whenever its backend happens
  to be owned by a granted repo.

- **MATCHES_STATE disjunct's "at most one edge" invariant closed a real
  tenant-visibility leak, not just a theoretical one (#5623 P0 review
  follow-up)** — the MATCHES_STATE disjunct above assumes a
  `TerraformStateResource` has at most one `MATCHES_STATE` edge, which depends
  on `terraformStateMatchesConfigEdgeRetractStatements`
  (`go/internal/storage/cypher/tfstate_state_match_edge_retract.go`) deleting
  the old edge whenever a state resource's resolved owning repo changes --
  including on delta cycles, not only full reconciliation. Do not reintroduce a
  `DeltaProjection` skip on that retract: the retract's own `s.generation_id =
  $generation_id` anchor already restricts it to state resources upserted THIS
  exact generation, so it is safe to run on every cycle after the first.

- **The delta-cycle retract fix above wiped a still-valid edge on an ordinary
  resolver hiccup (#5623 P1 review follow-up)** —
  `terraformStateMatchesConfigEdgeRetractStatements`'s `s.generation_id =
  $generation_id` anchor proves only "this generation upserted the node," not
  "we know its correct owner this cycle": a transient resolver failure also
  leaves `OwningRepoID == ""`, indistinguishable from a genuine ownership
  change. The retract's `s.uid IN $uids` set must include only rows whose
  `OwningRepoID` actually resolved THIS cycle (non-empty), batched by
  `w.batchSize` mirroring `terraformStateResourceMigrationStatements`'s own
  uid-batching precedent (`tfstate_canonical_writer_retract.go`) -- a resolver
  hiccup must not retract a still-valid edge.

- **NoOwner/AmbiguousOwner must retract too, not just Resolved (#5623 P1 review
  follow-up to the fix above)** — the `row.OwningRepoID != ""` filter above
  also wrongly excluded two AUTHORITATIVE non-owner answers
  (`tfstatebackend.ErrNoConfigRepoOwnsBackend`,
  `tfstatebackend.ErrAmbiguousBackendOwner`) that also leave `OwningRepoID`
  empty, letting a backend that became unowned or ambiguous keep its former
  owner's `MATCHES_STATE` edge indefinitely.
  `TerraformStateOwnershipResolver.ResolveOwningRepoID` returns a four-value
  `projector.TerraformStateOwnershipOutcome` enum (Resolved, TransientFailure,
  NoOwner, AmbiguousOwner); the retract's uid filter must exclude only
  `TerraformStateOwnershipTransientFailure` -- Resolved, NoOwner, and
  AmbiguousOwner are all retract-eligible.

- **Provisioning-candidate read gained a deterministic, structurally bounded LIMIT (#5720)** — see `evidence-5720-provisioning-candidate-bound.md`, continued in `evidence-5720-truncation-enumeration.md` for what belongs on the consumer-truncation enumeration and why.

## Common changes and how to scope them

- **Add a new HTTP handler** → create a handler struct with `Neo4j GraphQuery`
  and/or `Content ContentStore` fields, add a `Mount(mux *http.ServeMux)` method
  with explicit `mux.HandleFunc` calls, add the struct field to `APIRouter`
  (`handler.go:110`), call `Mount` in `APIRouter.Mount` (`handler.go:125`), wire
  the concrete adapter in `cmd/api/wiring.go`'s `newRouter`, add a
  `openapi_paths_*.go` fragment and reference it in `OpenAPISpec()`, update
  `docs/public/reference/http-api.md`. Run
  `go test ./cmd/api ./internal/query -count=1`. Why: missing any step leaves a
  route reachable but not documented, not gated, or not wired to the right
  adapter.

- **Add a new capability** → add an entry to `capabilityMatrix` in `contract.go`
  with per-profile max truth levels; add the capability ID constant near the
  existing `const` blocks if reused across handlers; call `BuildTruthEnvelope`
  with the new ID in the handler; update `specs/capability-matrix.v1.yaml` or a
  small fragment under `specs/capability-matrix/`, plus
  `docs/public/reference/http-api.md`. Run `go test ./internal/query -count=1`
  (the `contract_matrix_test.go` validates matrix coverage). Why:
  `BuildTruthEnvelope` panics on unknown capability IDs at handler call time.

- **Change a response shape** → update the handler method, the matching
  `openapi_paths_*.go` string constant, and `docs/public/reference/http-api.md` in
  the same PR. Why: the OpenAPI spec is a static string; it does not reflect from
  Go structs automatically.

- **Add a new graph query** → write the Cypher in the handler or a helper file
  named after the domain (`repository_*.go`, `code_*.go`); call
  `Neo4jReader.Run` or `RunSingle`; use `StringVal`, `BoolVal`, `IntVal` to
  extract row values; add an OTEL span via `startQueryHandlerSpan` if the query
  represents a distinct user-visible capability. Why: consistent span attributes
  (`http.route`, `eshu.capability`) let operators correlate latency metrics to
  specific capabilities.

- **Change structural inventory** → keep normal prompt flow on
  `content_entities` through `ContentReader` unless a prompt truly needs graph
  relationships. The route must keep repo/path/language/type filters, bounded
  `limit+1` probing, deterministic ordering, truncation metadata, and source
  handles.

- **Change import dependency investigation** → keep normal import, package,
  direct Python file-cycle, and cross-module call prompts on
  `POST /api/v0/code/imports/investigate`. Require at least one repo/file/module
  scope anchor before expanding `IMPORTS` or `CALLS`. Keep one connected graph
  pattern per read, deterministic ordering, caller-page `limit+1` probing, and
  the 25,001-row internal sentinel that fails closed above 25,000 candidates.
  Anchor module membership from the exact module name and preserve repository
  plus file-path identity through paging. For cycle reads, apply directional
  file and module filters only after reciprocal edges are reconstructed.
  Reject negative paging bounds and return exactly one row key for each query
  type (`dependencies`, `modules`, `cycles`, or `cross_module_calls`) plus
  source handles for file drill-down.

- **Change call graph metrics** → keep recursive-function and hub-function
  prompts on `POST /api/v0/code/call-graph/metrics`. Require `repo_id` before
  expanding `CALLS`, keep deterministic ordering plus `limit+1` truncation
  probing, reject negative paging bounds, and return canonical `functions` rows
  with source handles, hub call-degree counts, and recursion evidence.

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
  capability → add the missing entry to `capabilityMatrix` in `contract.go:134`
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
  `GraphQuery`, not `neo4jdriver.DriverWithContext`. Only `neo4j.go`,
  `neo4j_read_policy.go`, and `wiring.go` should import the Neo4j driver.
  `neo4j_read_policy.go` (added #5273) is the universal bounded-read policy and
  is the only driver-import-allowed file added for graph-read deadlines; new
  handler or query-layer code must route reads through it, not the driver.

- **Adding public routes to `publicHTTPPaths` without review** — the map in
  `auth.go:10` bypasses bearer-token auth. Adding a data route here exposes it
  without authentication.

- **Using `panic` for profile-gate failures** — use `WriteContractError` with
  `ErrorCodeUnsupportedCapability` and the structured `ErrorProfiles` fields.
  Panics are reserved for programmer errors like missing capability matrix entries.

## What NOT to change without an ADR

- `capabilityMatrix` entry `RequiredProfile` values — these gate which runtime
  profiles can answer which queries; changes affect CLI, MCP, and HTTP clients
  simultaneously; see `docs/public/reference/http-api.md` and
  `specs/capability-matrix.v1.yaml` plus `specs/capability-matrix/*.yaml`.
- `ResponseEnvelope` and `TruthEnvelope` field names — these are stable wire
  contracts used by MCP tool dispatch and CLI `--json` mode; see
  `docs/public/reference/http-api.md`.
- `EnvelopeMIMEType` (`application/eshu.envelope+json`) — changing this MIME type
  breaks every client that has already adopted envelope negotiation.

## Evidence and change history

The dated, per-issue evidence log for this package is split across four files
to keep every file under CLAUDE.md's 500-line-per-file convention (not
repo-enforced):
[AGENTS-evidence-history.md](AGENTS-evidence-history.md) (part 1, #2048
through #4794/#4733),
[AGENTS-evidence-history-2.md](AGENTS-evidence-history-2.md) (part 2, #3492
through #5816, plus a P1 follow-up to #5764),
[AGENTS-evidence-history-3.md](AGENTS-evidence-history-3.md) (part 3, #5761
and #5764), and [AGENTS-evidence-history-4.md](AGENTS-evidence-history-4.md)
(part 4, #5764 rounds 6-9). Entries are either preserved with headings
demoted one level (`##` to `###`) from a single-file AGENTS.md, or split out
of this file's own "Invariants this package enforces" section above: the
normative rule sentence for each of the following stays here, and its
No-Regression/Observability evidence lives in the linked history part --
registry bundle search scoping (#3493, #3506, #3520), incident-context typed
decode and work-item evidence pagination (#4794/#4733), package-registry
correlation pagination and authz-gate invariants (#5461/#5816), the
dependency-chains publisher-truncation signal (#5816), the language-query
route-level capability and error mapping (#5761), and the repository
context/story auxiliary graph-read degrade rationale for #5764, including
its round-6 through round-9 review follow-ups. #2048-#2110/#2225 and
#3492/#3507/#3519/#5623 split the same way but already have bullets above,
so are omitted here. Read the relevant part before touching a route, helper,
or call site one of those issue numbers names. Add new dated per-issue
entries to whichever part is closest in issue number, not in AGENTS.md.
