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
   `SpanQueryIaCUnmanagedResources`, `SpanQueryInfraResourceSearch`,
   `SpanQueryCodeStructuralInventory`, `SpanQueryCodeTopicInvestigation`,
   `SpanQueryDeadCodeInvestigation`) and log key conventions; check here
   before adding new telemetry.

## Invariants this package enforces

- **Capability gate before any read** — handlers call the unexported
  `capabilityUnsupported` helper before touching `GraphQuery` or `ContentStore`.
  A nil max-truth means the capability is blocked at the current profile.
  `capabilityUnsupported` consults the `capabilityMatrix` map in `contract.go:134`
  which stores `TruthLevelExact` and `TruthLevelDerived` ceiling values per
  profile. On failure, handlers call `WriteContractError` (`handler.go:40`).

- **`BuildTruthEnvelope` panics on unknown capability** — every capability string
  passed to `BuildTruthEnvelope` must exist in `capabilityMatrix`
  (`contract.go:547`). Add the capability to the map before the handler is
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

- **Repository tenant-isolation canary evidence** — #2048 filters repository
  list and selector reads from `AuthContext` before pagination, counts,
  ambiguity, and not-found decisions. No-Regression Evidence:
  `go test ./internal/query -run
  'Test(RepositoryList.*ScopedAuth|ResolveRepositorySelector.*ScopedAuth|ResolveRepositorySelectorDenies|RepositoryListSharedAuth|RepositoryListAllScopeAdmin)'
  -count=1`. No-Observability-Change: the canary adds no route, graph write,
  metric, label, runtime knob, or response field; existing repository query
  spans, `repository_query.stage_*` logs, result limits, partial reasons, and
  truncation metadata diagnose the path.

- **Code search scoped-token route evidence** — #2062 opens only
  `POST /api/v0/code/search` after `CodeHandler` applies `AuthContext` bounds to
  repository selector ambiguity, graph search predicates, and content fallback
  calls. Scoped graph search adds the repository/scope-id predicate before
  `LIMIT`; scoped content fallback queries authorized repositories individually
  and never calls all-repository content methods. No-Regression Evidence:
  `go test ./internal/query -run
  'Test(CodeSearch|AuthMiddlewareWithScopedTokensAllowsCodeSearch)' -count=1`
  and `go test ./internal/mcp -run
  TestDispatchToolFindCodeAllowsScopedCodeSearchRoute -count=1`.
  No-Observability-Change: the route adds no graph write, metric label, runtime
  knob, or response field; existing code search truth envelopes, graph query
  spans, HTTP route attribution, and content-store Postgres spans diagnose the
  bounded read path.

- **Entity resolution scoped-token route evidence** — #2064 opens only
  `POST /api/v0/entities/resolve` after `EntityHandler` applies `AuthContext`
  bounds to selector ambiguity, graph entity predicates, repo-identity
  hydration, and content fallback calls. Scoped graph resolution adds the
  repository/scope-id predicate before `LIMIT`; scoped content fallback queries
  authorized repositories individually and never calls all-repository content
  methods. No-Regression Evidence: `go test ./internal/query -run
  'Test(ResolveEntity.*Scoped|ResolveEntity.*Grant|ResolveEntity.*AllScope|AuthMiddlewareWithScopedTokensAllowsEntityResolve)'
  -count=1` and `go test ./internal/mcp -run
  TestDispatchToolResolveEntityAllowsScopedEntityResolveRoute -count=1`.
  No-Observability-Change: the route adds no graph write, metric label, runtime
  knob, or response field; existing entity resolution truth envelopes, graph
  query spans, HTTP route attribution, and content-store Postgres spans diagnose
  the bounded read path.

- **Content scoped-token route evidence** — #2066 opens only the content
  file/entity read and search routes after `ContentHandler` applies
  `AuthContext` bounds to repository selector resolution, no-repo search scope,
  exact entity read repo checks, and empty-grant short-circuits. Scoped search
  uses authorized repository IDs before result counting and truncation; scoped
  exact reads return not found for out-of-grant rows without returning payload
  fields. No-Regression Evidence: `go test ./internal/query -run
  'Test(ContentHandlerScoped|ContentHandlerAllScope|AuthMiddlewareWithScopedTokensAllowsContentRoutes)'
  -count=1` and `go test ./internal/mcp -run
  TestDispatchToolSearchFileContentAllowsScopedContentSearchRoute -count=1`.
  No-Observability-Change: the route family adds no graph write, metric label,
  runtime knob, or response field; existing content-store `postgres.query`
  spans with `db.operation=get_file_content`, `get_file_lines`,
  `get_entity_content`, `search_file_content`, and `search_entity_content`,
  plus HTTP route attribution and truth envelopes, diagnose the bounded read
  path.

- **Evidence citation scoped-token route evidence** — #2068 opens only
  `POST /api/v0/evidence/citations` after `EvidenceHandler` applies
  `AuthContext` bounds to file-handle hydration and entity-result filtering.
  Empty scoped grants return zero resolved citations without content-store
  hydration; out-of-scope file handles are never sent to the file batch reader,
  and out-of-scope entity rows are treated as missing before citation payloads
  are built. No-Regression Evidence: `go test ./internal/query -run
  'Test(EvidenceHandler.*Citation.*(Scoped|AllScope)|AuthMiddlewareWithScopedTokensAllowsEvidenceCitationRoute)'
  -count=1` and `go test ./internal/mcp -run
  TestDispatchToolEvidenceCitationAllowsScopedCitationRoute -count=1`.
  No-Observability-Change: the route adds no graph write, metric label,
  runtime knob, or response field; existing `query.evidence_citation_packet`
  handler spans, content-store `postgres.query` spans, HTTP route attribution,
  and truth envelopes diagnose the bounded citation hydration path.

- **Entity context scoped-token route evidence** — #2070 opens only
  `GET /api/v0/entities/{entity_id}/context` after `EntityHandler` applies
  `AuthContext` bounds to empty grants, graph entity predicates, repo-identity
  hydration, and content fallback rows. Scoped graph context reads add an
  authorized repository predicate before relationship hydration; scoped content
  fallback treats out-of-grant entity rows as not found before reading
  relationships. No-Regression Evidence: `go test ./internal/query -run
  'Test(GetEntityContext.*Scoped|GetEntityContext.*Grant|GetEntityContext.*Fallback|AuthMiddlewareWithScopedTokensAllowsEntityContextRoute)'
  -count=1` and `go test ./internal/mcp -run
  TestDispatchToolEntityContextAllowsScopedEntityContextRoute -count=1`.
  No-Observability-Change: the route adds no graph write, metric label,
  runtime knob, or response field; existing entity context truth envelopes,
  graph query spans, HTTP route attribution, and content-store Postgres spans
  diagnose the bounded read path.

- **Service/workload context scoped-token route evidence** — #2072 opens only
  `GET /api/v0/workloads/{workload_id}/context`,
  `GET /api/v0/workloads/{workload_id}/story`,
  `GET /api/v0/services/{service_name}/context`, and
  `GET /api/v0/services/{service_name}/story` after `EntityHandler` applies
  `AuthContext` bounds to empty grants, workload lookup predicates, service
  candidate selection, repository selector disambiguation, and read-model
  fallback rows. No-Regression Evidence: `go test ./internal/query
  -run
  'Test(GetWorkload|GetService|ServiceWorkload|AuthMiddlewareWithScopedTokens)'
  -count=1` and `go test ./internal/mcp -run
  'TestDispatchTool(Service|Workload|ServiceAndWorkload)|TestEveryRegisteredToolHasDispatchRoute'
  -count=1`. No-Observability-Change: the route family adds no graph write,
  metric label, runtime knob, or response field; existing service query
  `service_query.stage_*` logs, graph query spans, HTTP route attribution,
  truth envelopes, result limits, and partial reasons diagnose the bounded
  read path.

- **Service investigation scoped-token route evidence** — #2074 opens only
  `GET /api/v0/investigations/services/{service_name}` after `EntityHandler`
  applies `AuthContext` bounds to empty grants, service candidate selection,
  repository selector disambiguation, environment filtering, read-model
  fallback rows, coverage metadata, and recommended next calls. MCP dispatch
  for `investigate_service` remains transport-only and forwards service,
  repository, and environment selectors through the shared HTTP handler.
  No-Regression Evidence: `go test ./internal/query -run
  'Test(AuthMiddlewareWithScopedTokensAllowsServiceInvestigationRoute|InvestigateService)'
  -count=1` and `go test ./internal/mcp -run
  TestDispatchToolInvestigateServiceAllowsScopedRoute -count=1`.
  No-Observability-Change: the route adds no graph write, metric label,
  runtime knob, or response field; existing service query `service_query.stage_*`
  logs, graph query spans, HTTP route attribution, truth envelopes, result
  limits, and partial reasons diagnose the bounded investigation read path.

- **Query playbook scoped-token route evidence** — #2076 opens only
  `GET /api/v0/query-playbooks` and `POST /api/v0/query-playbooks/resolve`
  because `QueryPlaybookHandler` reads deterministic in-process catalog data
  and never calls graph, Postgres, providers, collectors, or tenant data stores.
  Live-data route targets referenced by resolved playbook steps remain governed
  by their own scoped-route allowlist entries. No-Regression Evidence:
  `go test ./internal/query -run
  'Test(QueryPlaybookHandler|AuthMiddlewareWithScopedTokensAllowsQueryPlaybookRoutes)'
  -count=1` and `go test ./internal/mcp -run
  'Test(QueryPlaybook|TestDispatchToolQueryPlaybooksAllowsScopedRoutes)'
  -count=1`. No-Observability-Change: the route family adds no graph write,
  graph read, Postgres read, metric label, runtime knob, or response field;
  existing HTTP route attribution and query-playbooks truth envelopes diagnose
  the static catalog/resolver path.

- **Vulnerability scanner contract scoped-token route evidence** — #2078 opens
  only `GET /api/v0/supply-chain/vulnerability-scanner/contract` because
  `SupplyChainHandler.getVulnerabilityScannerReadContract` returns a
  deterministic in-process route/filter contract and never calls graph,
  Postgres, providers, collectors, repositories, tenants, or token stores. Live
  scanner findings, counts, inventories, explanations, and provider-alert
  routes stay governed by their own scoped-route allowlist entries.
  No-Regression Evidence: `go test ./internal/query -run
  'Test(VulnerabilityScannerReadContract|AuthMiddlewareWithScopedTokensAllowsScannerContractRoute)'
  -count=1` and `go test ./internal/mcp -run
  'Test(ResolveRouteMapsVulnerabilityScannerContract|DispatchToolScannerContractAllowsScopedRoute)'
  -count=1`. No-Observability-Change: the route adds no graph write, graph
  read, Postgres read, provider call, collector call, metric label, runtime
  knob, or response field; existing HTTP route attribution and scanner-contract
  truth envelopes diagnose the static route.

- **Hosted governance status scoped-token route evidence** — #2080 opens only
  `GET /api/v0/status/governance` because `StatusHandler.getGovernanceStatus`
  returns redacted runtime governance posture: normalized modes, safe revision
  hashes, booleans, and aggregate counts. Existing governance status tests
  prove policy bodies, private source IDs, credential handles, raw provider
  details, prompts, provider responses, private endpoint-like values, and local
  paths are not returned. The route does not read graph, content, repositories,
  supply-chain findings, provider payloads, collectors, raw tenants, raw
  workspaces, or token values. No-Regression Evidence: `go test
  ./internal/query -run
  'Test(StatusHandlerGovernance|GovernanceStatus|AuthMiddlewareWithScopedTokensAllowsGovernanceStatusRoute)'
  -count=1` and `go test ./internal/mcp -run
  'TestHostedGovernanceRuntimeToolRoutesToStatus|TestDispatchToolGovernanceStatusAllowsScopedRoute'
  -count=1`. No-Observability-Change: the route adds no graph write, graph
  read, content read, provider call, collector call, metric label, runtime knob,
  or response field; existing HTTP route attribution and governance status
  truth envelopes diagnose the redacted runtime readback path.

- **Semantic extraction status scoped-token route evidence** — #2082 opens only
  `GET /api/v0/status/semantic-extraction` because
  `StatusHandler.getSemanticExtractionStatus` returns redacted runtime semantic
  extraction posture: provider availability state, source-class enablement,
  deterministic-path impact, supported enum values, aggregate queue counts,
  budget counters, and audit class counts. Provider profile detail text stays
  out of the response; raw prompts, provider responses, credential handles,
  token values, private endpoints, tenant/workspace ids, repository/source ids,
  graph reads, content reads, and provider payloads remain outside the status
  route. No-Regression Evidence: `go test ./internal/query -run
  'Test(StatusHandlerSemanticExtraction|AuthMiddlewareWithScopedTokensAllowsSemanticExtractionStatusRoute)'
  -count=1` and `go test ./internal/mcp -run
  'TestSemanticCapabilityRuntimeToolRoutesToStatus|TestDispatchToolSemanticExtractionStatusAllowsScopedRoute'
  -count=1`. No-Observability-Change: the route adds no graph write, graph
  read, content read, provider call, collector call, metric label, runtime knob,
  or response field; existing HTTP route attribution and semantic extraction
  status truth envelopes diagnose the redacted runtime readback path.

- **Component extension scoped-token route evidence** — #2084 opens only
  `GET /api/v0/component-extensions` and
  `GET /api/v0/component-extensions/{component_id}/diagnostics` because
  `ComponentExtensionsHandler` returns bounded local component registry posture:
  package ids, names, publishers, versions, manifest digests, lifecycle states,
  activation config handles, trust-policy booleans, and stable policy/error
  codes. Local manifest paths, activation config paths, raw component config,
  registry file paths, credentials, endpoints, tenant/workspace ids, repository
  ids, graph reads, content reads, and provider payloads remain outside the
  response. No-Regression Evidence: `go test ./internal/query -run
  'Test(ComponentExtensionsHandler|AuthMiddlewareWithScopedTokensAllowsComponentExtensionRoutes)'
  -count=1` and `go test ./internal/mcp -run
  'Test(ComponentExtensionToolsResolveToQueryRoutes|TestDispatchToolComponentExtensionsAllowsScopedRoutes)'
  -count=1`. No-Observability-Change: the route adds no graph write, graph
  read, content read, provider call, collector call, metric label, runtime knob,
  or response field; existing HTTP route attribution and component-extension
  truth envelopes diagnose the bounded local registry readback path.

- **Hosted readiness scoped-token route evidence** — #2090 opens only
  `GET /api/v0/status/hosted-readiness` because `StatusHandler` returns
  bounded hosted readiness checks, queue counters, repository count, diagnostic
  route names, and aggregate coordinator counters. Scoped responses replace
  coordinator instance rows with `scopedCoordinatorToMap`, so collector instance
  ids, display names, tenant/workspace values, queue conflict keys,
  repository/source ids, graph row detail, provider payloads, local paths, and
  credentials stay outside the payload. No-Regression Evidence: `go test
  ./internal/query -run
  'Test(AuthMiddlewareWithScopedTokensAllowsHostedReadinessRoute|StatusHandlerHostedReadiness)'
  -count=1` and `go test ./internal/mcp -run
  'TestDispatchToolHostedReadinessAllowsScopedRoute' -count=1`.
  No-Observability-Change: the route adds no graph write, content read,
  provider call, collector call, metric label, runtime knob, or response field
  for shared-token callers; existing HTTP route attribution and hosted readiness
  status fields diagnose the bounded status readback path.

- **Collector status scoped-token route evidence** — #2088 opens only
  `GET /api/v0/status/collectors` because `StatusHandler.listCollectors`
  returns aggregate runtime posture for scoped tokens: collector kind,
  runtime/category/health buckets, collector counts, coordinator/enabled/
  bootstrap/claim counts, evidence-source summaries, observation counts, and
  aggregate timestamps. Scoped responses do not expose collector instance ids,
  display names, source systems, detail text, tenant/workspace values, queue
  conflict keys, repository/source ids, graph reads, content reads,
  credentials, endpoints, local paths, or provider payloads. The legacy
  `/api/v0/collectors` route remains fail-closed for scoped tokens.
  No-Regression Evidence: `go test ./internal/query -run
  'Test(AuthMiddlewareWithScopedTokensAllowsCollectorStatusRoute|StatusHandler)'
  -count=1` and `go test ./internal/mcp -run
  'Test(ListCollectorsRuntimeToolRoutesToStatusCollectors|DispatchToolCollectorStatusAllowsScopedRoute)'
  -count=1`. No-Observability-Change: the route adds no graph write, graph
  read, content read, provider call, collector call, metric label, runtime knob,
  or response field for shared-token callers; existing HTTP route attribution
  and runtime status fields diagnose the bounded status readback path.

- **Ingester status scoped-token route evidence** — #2086 opens only
  `GET /api/v0/status/ingesters` and
  `GET /api/v0/status/ingesters/{ingester}` because `StatusHandler` returns
  bounded runtime health, queue, scope-activity, stage-summary, domain-backlog,
  and coordinator aggregate counters for the repository ingester. Scoped-token
  detail responses replace coordinator instance rows with aggregate counts so
  collector instance ids, display names, tenant/workspace values, queue conflict
  keys, repository/source ids, graph reads, content reads, credentials,
  endpoints, local paths, and provider payloads remain outside the payload.
  No-Regression Evidence: `go test ./internal/query -run
  'Test(AuthMiddlewareWithScopedTokensAllowsIngesterStatusRoutes|StatusHandler)'
  -count=1` and `go test ./internal/mcp -run
  'Test(ListIngestersRuntimeToolRoutesToStatusIngesters|GetIngesterStatusRuntimeToolRoutesToRepositoryStatus|DispatchToolIngesterStatusAllowsScopedRoutes)'
  -count=1`. No-Observability-Change: the route adds no graph write, graph
  read, content read, provider call, collector call, metric label, runtime knob,
  or response field for shared-token callers; existing HTTP route attribution
  and runtime status fields diagnose the bounded status readback path.

- **Semantic evidence scoped-token route evidence** — #2110 opens only
  `GET /api/v0/semantic/documentation-observations` and
  `GET /api/v0/semantic/code-hints` after `SemanticEvidenceHandler` intersects
  the fact-record read model with `AuthContext` repository/scope grants before
  ordering and paging. Empty grants return empty pages without broad fact reads.
  No-Regression Evidence: `go test ./internal/query -run
  'Test(AuthMiddlewareWithScopedTokensAllowsSemanticEvidenceRoutes|SemanticEvidenceHandlerScopedEmptyGrantReturnsEmptyWithoutRead|BuildSemanticEvidenceSQL.*Semantic)' -count=1`
  and `go test ./internal/mcp -run
  'Test(DispatchToolSemanticEvidenceAllowsScopedRoutes|SemanticEvidenceToolsRouteToBoundedHTTPReads)' -count=1`.

- **Package registry reads stay anchored** — `PackageRegistryHandler` in
  `package_registry.go` must require `limit` plus a route-specific anchor
  before graph reads: package lookups use `package_id` or `ecosystem`, version
  lookups use `package_id`, and dependency lookups use `package_id` or
  `version_id`. Do not add whole-graph package scans, and do not present
  package source hints as ownership, publication ownership, or runtime
  consumption truth. Scoped tokens may read only package-registry correlations
  after repository/scope grants are applied before ordering and limits; adjacent
  package identity, version, dependency, count, and inventory routes stay
  fail-closed until each has route-specific proof. Scoped tokens may read CI/CD
  run correlation list/count/inventory routes only after repository/scope grants
  are applied before ordering, limits, grouping, offsets, and truncation; README
  carries the focused no-regression and no-observability markers.

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

- **Keep dead-code investigation conservative for JavaScript/TypeScript** —
  `handleDeadCodeInvestigation` buckets JavaScript, JSX, TypeScript, and TSX
  active candidates as `ambiguous` until issue #336 records corpus precision
  evidence. Do not move those candidates into `cleanup_ready` based only on a
  missing incoming graph edge.

- **SQL routine reachability uses graph `EXECUTES` probes** —
  `CodeHandler.filterDeadCodeResultsWithoutIncomingEdges` falls through to
  `deadCodeResultsWithGraphIncomingEdges` for `SqlFunction` candidates
  (`code_dead_code_scan.go:128`, `code_dead_code_scan.go:240`) because SQL
  relationship materialization graph-writes `EXECUTES` edges directly instead
  of storing completed shared-projection intent rows. Keep the probe batched;
  reverting to one graph call per SQL routine can make large dead-code pages
  too expensive, while removing the fallback can report trigger-bound SQL
  routines as cleanup candidates.

- **`neo4j_read_policy.go` owns the read session lifecycle** — `Run` and
  `RunSingle` delegate to `runReadAttempts`, which opens and closes a session
  per attempt inside `runReadAttempt`. A single logical read can open up to 2
  sessions (one bounded retry via `maxGraphReadAttempts`). Do not hold or share
  sessions across handler calls.

- **`analyze_infra_relationships` honors `relationship_type` (#3492)** —
  `getRelationships` (`infra_relationship_filter.go`) decodes the optional
  `relationship_type` and resolves it through `resolveInfraRelationshipTypes`'s
  fixed allowlist (semantic MCP aliases `what_deploys` / `what_provisions` /
  `module_consumers` / `who_consumes_xrd` / `what_runs_image` plus canonical
  edge types such as `DEPLOYS_FROM`, `USES_MODULE`, `RUNS_IMAGE`). The resolved
  types render into the
  `OPTIONAL MATCH (n)-[r:TYPE_A|TYPE_B]->(...)` pattern as an inline
  relationship-type filter. An empty argument keeps the prior bare untyped
  pattern (whole-relationship); an unrecognized value returns 400. Do not feed
  free-text into the inline clause — only allowlisted edge-type names may render
  there. Do not add an alias that maps to an edge type the graph does not write.

  No-Regression Evidence: the filter narrows the single per-entity relationship
  read; it neither widens the seed-node lookup nor adds a query. Baseline = the
  pre-#3492 bare `OPTIONAL MATCH (n)-[r]->(target)` / `(source)-[r2]->(n)`
  whole-relationship pattern; after = the same pattern with an inline
  `[r:TYPE...]` relationship-type predicate. Backend NornicDB (default canonical
  graph backend per AGENTS.md), Neo4j compatibility mode unaffected. Input shape:
  one `RunSingle` anchored on `n.id = $entity_id`, returning two collected
  relationship slices for a single entity; the filter can only shrink the matched
  edge set, so terminal row/queue counts are unchanged or lower and no extra
  round trip is added. The inline relationship-type predicate is index-served by
  the NornicDB relationship-type index, the same shape the relationships-catalog
  count/edge routes already rely on. Proof: `go test ./internal/query -run
  'TestInfraRelationships|TestResolveInfraRelationshipTypes' -count=1` (filtered
  vs unfiltered Cypher, scoped-token coexistence, 400 on unknown) and `go test
  ./internal/query ./internal/mcp ./internal/relationships ./internal/telemetry
  -count=1`.

  Observability Evidence: the read now opens span `query.infra_relationships`
  (`telemetry.SpanQueryInfraRelationships`, registered in
  `telemetry/registry.go` and pinned by `telemetry.TestSpanNames`) carrying the
  stable `http.route` / `eshu.capability` attributes plus a low-cardinality
  `eshu.relationship_filter` attribute (`all` when unfiltered, else the
  pipe-joined edge types) so an operator can confirm at 3 AM whether a
  `relationship_type` argument narrowed the read. No new metric label is added;
  the filter value is a span attribute, not a metric dimension, so cardinality
  stays bounded.

- **`what_deploys` spans the full deployment edge family (#3507)** — the #3492
  alias mapped `what_deploys` to `DEPLOYS_FROM` only, which dropped the runtime
  deployment topology the pre-#3492 untyped read surfaced — notably the
  `WorkloadInstance-[:DEPLOYMENT_SOURCE]->Repository` edge written by
  `canonicalDeploymentSourceUpsertCypher` and read by
  `fetchDeploymentSourcesFromGraph` (`impact_trace_deployment.go`). For a
  workload-instance target the tool could then report an empty deployment
  relationship even when the deployment-source edge exists. `what_deploys` now
  resolves to `{DEPLOYS_FROM, DEPLOYMENT_SOURCE, HAS_DEPLOYMENT_EVIDENCE}` — the
  same deploy family `entity_map_response.go` groups as "deployed by" plus the
  runtime deployment-source edge. When adding a new deployment edge type to the
  graph, add it here too so the deploy alias stays complete.

  No-Regression Evidence: pure accuracy fix; the change only widens the
  `what_deploys` alias edge-type set, it adds no query and changes no other
  alias. Baseline = `what_deploys` filtering to `[r:DEPLOYS_FROM]` (dropping
  `DEPLOYMENT_SOURCE`); after = `[r:DEPLOYS_FROM|DEPLOYMENT_SOURCE|HAS_DEPLOYMENT_EVIDENCE]`.
  Backend NornicDB (Neo4j compat unaffected); input shape unchanged — one
  `RunSingle` anchored on `n.id = $entity_id` returning two collected
  relationship slices for one entity. A wider type-union in the inline pattern is
  still index-served by the NornicDB relationship-type index and only changes
  which edges match; it adds no round trip and no broad scan, so terminal
  row/queue counts stay bounded by the same single-entity neighborhood. Proof:
  `go test ./internal/query -run
  'TestWhatDeploysSurfacesRuntimeDeploymentSourceEdge|TestResolveInfraRelationshipTypes|TestInfraRelationships'
  -count=1` (failing-first DEPLOYMENT_SOURCE regression) and `go test
  ./internal/query ./internal/mcp -count=1`.

  No-Observability-Change: reuses the #3492 span `query.infra_relationships` and
  its `eshu.relationship_filter` attribute (now reports the wider pipe-joined
  deploy set for `what_deploys`); no new span, metric, label, or log is added.

- **Scope predicate admits the deployment-source topology (#3519)** — widening
  `what_deploys` (above) surfaced a second scope bug: `infraResourceScopePredicate`
  (`infra_resource_aggregates.go`) authorized a neighbor only by `repo_id` or the
  `(:Repository)-[:DEFINES]->(:Workload)<-[:INSTANCE_OF]-(:WorkloadInstance)-[:USES]->(n)`
  USES path. A `DEPLOYMENT_SOURCE` edge points from an in-grant `WorkloadInstance`
  to a `Repository` node; the Repository neighbor carries `id`, not `repo_id`, and
  is not a USES target, so the scope-filtered relationship read dropped the edge
  under a scoped token even when the repository was in grant. The predicate now
  also admits a node whose own `id` is a granted repository (`n.id IN $allowed_*`,
  for the Repository neighbor) and a `WorkloadInstance` anchored to a granted repo
  via `(:Repository)-[:DEFINES]->(:Workload)<-[:INSTANCE_OF]-(n)` (no USES hop, for
  a deployment-source seed/instance). Both new disjuncts are anchored on the
  indexed `Repository.id` grant filter, and node ids are namespaced (`repo:` vs
  `tf:`/`k8s:`/`workload:`), so the `id`-grant disjunct is inert for non-Repository
  nodes and never widens their authorization. Do not drop the label/anchor shape:
  matching a bare `id` without the `Repository`/`DEFINES` anchor would risk
  authorizing a cross-tenant node by id collision.

  No-Regression Evidence: scope-correctness fix; the predicate gains two
  fail-closed disjuncts and removes none. Baseline = predicate with `repo_id` +
  USES-path disjuncts (dropping in-grant `DEPLOYMENT_SOURCE` Repository
  neighbors); after = same plus `id`-grant and DEFINES/INSTANCE_OF disjuncts.
  Backend NornicDB (Neo4j compat unaffected); the predicate renders only in
  scoped mode, so unscoped infra search/aggregate/relationship Cypher is
  byte-identical and unchanged. The new EXISTS subquery mirrors the existing
  workloadScopePredicate DEFINES anchor (indexed `Repository.id`), adding no
  unbounded scan; it runs per candidate neighbor exactly like the existing USES
  disjunct, so the bounded single-entity neighborhood cost class is unchanged.
  Proof: `go test ./internal/query -run
  'TestScopedWhatDeploys|TestInfraResourceScopePredicateAdmitsDeploymentTopology'
  -count=1` (in-grant edge returned, out-of-grant excluded, predicate shape
  pinned) and `go test ./internal/query ./internal/mcp -count=1`.

  No-Observability-Change: the scope predicate is a Cypher WHERE fragment with no
  span, metric, label, or log surface; the relationship read still reports the
  #3492 `query.infra_relationships` span and `eshu.relationship_filter` attribute.

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

## Edge resolution provenance surfacing (#2225)

The relationship-story reads (`code_relationship_story_graph.go`,
`code_relationship_story_nornicdb.go`) return `rel.confidence as confidence` and
`rel.resolution_method as resolution_method` so `CALLS`/`REFERENCES` edges carry
the per-edge provenance written under ADR #2222. `relationshipStoryRowsWithHandles`
drops both keys when nil/empty so legacy edges omit them rather than surfacing a
null tier. The `Relationship` OpenAPI schema gains `resolution_method`.

No-Regression Evidence: `go test ./internal/query -run 'RelationshipStory|OpenAPI' -count=1`
and `go test ./internal/query ./internal/mcp ./cmd/api -count=1` pass;
`TestHandleRelationshipStorySurfacesEdgeProvenance` fails before the RETURN and
row-shaping changes and passes after. The change adds two scalar projections to
the existing relationship-story RETURN — no new `MATCH`, traversal, `ORDER BY`,
or pagination shape, and the bounded `SKIP`/`LIMIT` are unchanged — so the read
plan is invariant on both Neo4j and NornicDB.

No-Observability-Change: this change adds no route, graph write, queue, worker,
runtime knob, metric instrument, or metric label. Operators still diagnose the
relationship-story read through the existing `neo4j.query` spans, query-duration
metrics, and the answer-level `TruthEnvelope`; per-edge provenance rides as two
additive response fields.

## Registry bundle search targets the package registry catalog (#3493)

`handleSearchBundles` (`code_cypher.go`, `POST /api/v0/code/bundles`, MCP
`search_registry_bundles`) previously ran `MATCH (r:Repository) WHERE r.name
CONTAINS $query` — a repository-name search behind a registry/SBOM-bundle name.
It now searches the pre-indexed package registry catalog via
`searchRegistryBundlesCypher`: `MATCH (p:Package)` filtered by a bound,
case-insensitive substring over `normalized_name`, `namespace`, or `purl`, with
an optional `ecosystem` scope, and returns `package_id`, `name`, `ecosystem`,
`registry`, `namespace`, `purl`, and `version_count`. The `:Package` nodes carry
the dual `:PackageRegistryPackage` label written by the reducer, so this reads
real registry data the way `list_package_registry_*` does.

No-Regression Evidence: `go test ./internal/query ./internal/mcp -count=1` pass;
`TestHandleSearchBundles_SearchesRegistryPackages` and
`TestHandleSearchBundles_ScopesByEcosystem` fail before the rewrite (the handler
emitted `:Repository`/`r.repo_id`) and pass after. The query keeps the same
bounded shape as before — a single anchored `MATCH` with substring predicates,
deterministic `ORDER BY p.ecosystem, p.normalized_name, p.uid`, and `LIMIT
$limit` (`limit+1` truncation probe) — and `:Package` already backs the
`list_package_registry_packages` read path, so the plan selectivity is no worse
than the prior `:Repository` scan; no measurable regression for a correctness
fix on the same call shape.

No-Observability-Change: this change adds no route, graph write, queue, worker,
runtime knob, metric instrument, or metric label. The route keeps its existing
`cypherQueryTimeout`-bounded context, the `platform_impact.context_overview`
`TruthEnvelope`, and HTTP route attribution; only the request now also accepts an
optional `ecosystem` field and the response rows carry package identity instead
of repository identity.

## Registry bundle search requires a scope (#3506 follow-up)

`handleSearchBundles` now rejects a request that supplies neither `query` nor
`ecosystem` with `400` before any graph read, and the request body is required
(OpenAPI `requestBody.required: true`, `anyOf` of `query`/`ecosystem`). #3493
left the scope optional, so a catalog-head request anchored on
`MATCH (p:Package)` would scan every package and run the
`OPTIONAL MATCH (p)-[:HAS_VERSION]->(v)`/`count(v)` aggregation across the whole
registry before `LIMIT`, violating the bounded MCP/API read contract on large
registries. Requiring a non-empty scope keeps the read bounded by construction:
the produced Cypher always carries a selective `$query` or `$ecosystem`
predicate ahead of the version aggregation. The MCP tool schema keeps the
constraint in its property descriptions and handler validation rather than a
top-level `anyOf`, because exported MCP schemas must avoid OpenAI-restricted
top-level keywords.

No-Regression Evidence: `go test ./internal/query ./internal/mcp -count=1` pass;
`TestHandleSearchBundles_RequiresScope` (table of unscoped bodies) and
`TestSearchRegistryBundlesCypherAlwaysScoped` fail before this change (unscoped
requests returned `200` and queried the graph) and pass after. This strictly
narrows the input domain — scoped requests run the identical bounded query
shape, and the previously-unbounded unscoped path is removed — so there is no
regression for any request that already carried a scope.

No-Observability-Change: this change adds no route, graph write, queue, worker,
runtime knob, metric instrument, or metric label. Scoped requests keep the
existing `cypherQueryTimeout`-bounded context, `platform_impact.context_overview`
`TruthEnvelope`, and HTTP route attribution.

## Registry bundle scope validation rides the envelope (#3520 follow-up)

Two refinements close the gap between the bundle handler and its advertised
contract:

- `handleSearchBundles` now returns its scope/parse/backend errors through
  `writeSearchBundlesError`, which emits a canonical `ResponseEnvelope` with a
  populated `Error` (code `invalid_argument` / `internal_error`, capability
  `platform_impact.context_overview`) when the caller accepts
  `application/eshu.envelope+json`, and a plain error otherwise. The MCP dispatch
  path sets that Accept header and recognizes only canonical envelopes
  (`parseCanonicalEnvelope`); a non-envelope `400` there degraded to a transport
  error (`HTTP 400: ...`) instead of a structured `IsError` tool result. Mirrors
  the sibling `writeCypherQueryError` helper.
- The OpenAPI request schema and the MCP tool property schema add `minLength: 1`
  and a `pattern` of `\S` to `query` and `ecosystem`, so generated clients and
  docs reject empty or whitespace-only scope the same way the trimming handler
  does. The MCP additions are property-level only; the schema keeps no top-level
  `anyOf`/`oneOf`/`allOf` so the OpenAI-restricted-keyword contract test stays
  green.

No-Regression Evidence: `go test ./internal/query ./internal/mcp -count=1` pass.
`TestHandleSearchBundles_UnscopedReturnsEnvelopeError` and
`TestDispatchToolSearchRegistryBundlesUnscopedReturnsEnvelopeIsError` fail before
the helper (the handler emitted a plain `{error, detail}` body, so the HTTP test
could not unmarshal a `ResponseEnvelope` and the MCP dispatch returned an
`HTTP 400` transport error) and pass after.
`TestOpenAPISearchBundlesRejectsEmptyScope` fails before the schema gains
`minLength`/`pattern` and passes after. This only narrows accepted input and
changes error encoding; the success query shape is unchanged.

No-Observability-Change: no route, graph write, queue, worker, runtime knob,
metric instrument, or metric label is added. The error envelope reuses the
existing `ResponseEnvelope`/`ErrorEnvelope` contract and HTTP route attribution.

## Incident-context typed decode (#4794 W2a) and work-item evidence pagination fix (#4733)

`incident_context_decode.go`, `incident_context_routing_store.go`, and
`incident_context_runtime_store.go` decoded `incident.record`,
`incident.lifecycle_event`, `change.record`,
`incident_routing.applied_pagerduty_resource`,
`incident_routing.observed_pagerduty_service`,
`incident_routing.coverage_warning`, and `service_catalog.operational_link`
fact payloads with raw `StringVal`/`BoolVal` map lookups, silently defaulting
every field to `""`/`false` on a missing key instead of dead-lettering. They
now decode through new query-layer seams in `factschema_decode_incident.go`
(`decodeIncidentRecord`, `decodeIncidentLifecycleEvent`, `decodeChangeRecord`,
`decodeIncidentRoutingAppliedPagerDutyResource`,
`decodeIncidentRoutingObservedPagerDutyService`,
`decodeIncidentRoutingCoverageWarning`,
`decodeServiceCatalogOperationalLink`), mirroring the `work_item` family's
`factschema_decode_workitem.go` template: a row whose payload is missing a
required identity field is dropped (a `*queryDecodeError`, logged via
`logIncidentContextDecodeDrop`) rather than surfaced with an empty identity.
`incident.record`, `incident.lifecycle_event`, and `change.record` preserve one
pre-existing, tested behavior: when the payload omits its required identity key
entirely (`provider_incident_id` / `provider_event_id` / `provider_change_id`)
but the fact's durable `source_record_id` is present, the decode retries with
that id injected as the fallback identity (`incidentIdentityFallback`) instead
of dead-lettering — see
`TestPostgresIncidentContextStoreReadsCollectedPagerDutyIncidentBySourceRecordID`.
`reducer_ci_cd_run_correlation` and `reducer_kubernetes_correlation` (read in
`incident_context_runtime_store.go`) are reducer-derived facts with no
`factschema.FactKind*` constant and no Decode seam, so they stay on raw payload
reads — out of scope for this conversion (see the reducer-derived fact
governance decision, PR #4809). `go/internal/payloadusage/schema.go`'s
`factKindSchemaFile` gains `FactKindIncidentLifecycleEvent`,
`FactKindChangeRecord`, and `FactKindServiceCatalogOperationalLink` because
these seams are now visible to the merged reducer+query payload-usage manifest
gate (`go test ./internal/reducer -run TestPayloadUsageManifest`).

Separately, `WorkItemEvidenceStore.ListWorkItemEvidence` (`work_item_evidence.go`,
`work_item_evidence_store.go`, `work_item_evidence_handler.go`) fixes #4733: the
handler fetched `limit+1` facts, decoded the WHOLE window, and computed
`truncated := len(decodedRows) > limit`. A fact dropped mid-window by a failed
typed decode shrank the decoded count below the fetch count, so a genuinely
truncated page could report `truncated: false` with no `next_cursor`, hiding
evidence beyond the malformed fact, and could leak the `+1` lookahead fact into
the visible page. `ListWorkItemEvidence` now returns a `WorkItemEvidencePage`
(`Rows`, `Truncated`, `NextCursorFactID`) built by `buildWorkItemEvidencePage`,
which derives `Truncated`/`NextCursorFactID` from the RAW fetched fact count and
fact_id sequence — never from `len(Rows)` — and decodes only the visible
window (`fetchLimit-1` facts), so the lookahead fact can never leak into a page
whose earlier facts happened to drop.

No-Regression Evidence: `go test ./internal/query ./internal/mcp ./internal/payloadusage ./internal/reducer ./internal/projector -count=1` pass.
`TestBuildWorkItemEvidencePageDerivesTruncationFromFetchedFactsNotDecodedRows`
and `TestWorkItemListEvidenceHandlerAdvancesPastMalformedFactInsideWindow` fail
against the pre-fix `truncated := len(decodedRows) > limit` logic (proven by
temporarily reverting `buildWorkItemEvidencePage` to that shape) and pass after.
`TestDecodeIncidentContextIncidentDropsRowMissingBothIdentityFields`,
`TestDecodeIncidentContextTimelineEventDropsRowMissingBothIdentityFields`,
`TestDecodeIncidentContextChangeCandidateDropsRowMissingBothIdentityFields`,
`TestBuildIncidentAppliedPagerDutyRoutingDropsRowMissingRequiredField`,
`TestBuildIncidentObservedPagerDutyRoutingDropsRowMissingRequiredField`, and
`TestBuildIncidentRoutingCoverageWarningDropsRowMissingRequiredField` prove the
input_invalid drop per converted kind; the paired
`TestDecodeIncidentContextIncidentFallsBackToSourceRecordID` (and its
lifecycle-event/change-record siblings) and the `*DecodesValidPayload` tests
prove well-formed facts decode identically to the pre-conversion raw-map
output (same field mapping, same response shape) — this conversion is
output-preserving for valid facts. `TestPostgresIncidentContextStoreReadsCollectedPagerDutyIncidentBySourceRecordID`
and `TestPostgresIncidentContextStoreReturnsAmbiguousSourceRecordMatches` (both
pre-existing) continue to pass unchanged, proving the source_record_id fallback
path survived the conversion.

No-Observability-Change: neither change adds a route, graph write, metric
label, or runtime knob. The incident-context and work-item-evidence handlers
keep their existing `query.incident_context` / `work_item_evidence.list` truth
envelopes and HTTP route attribution; a dropped fact is visible via the
existing `slog.Debug` decode-drop log (fact_id, fact_kind, classification,
missing_field), matching the work_item family's established pattern.
