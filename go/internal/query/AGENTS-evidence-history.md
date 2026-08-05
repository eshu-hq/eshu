# internal/query evidence and change history

Dated, per-issue evidence and rationale entries for `go/internal/query`, split
out of [AGENTS.md](AGENTS.md) to keep the read-first guidance under
CLAUDE.md's 500-line-per-file convention (not a repo-enforced gate; no CI
check counts Markdown lines). Read the entry for a specific issue number before
touching the routes, helpers, or call sites that issue names. Add new dated
per-issue entries here (or to a sibling part below), not in AGENTS.md; keep
AGENTS.md itself the scoped-instructions surface (invariants, common changes,
failure modes, anti-patterns).

This is part 1 of 4, covering issues #2048 through #4794/#4733. Entries in
this part were originally authored directly in this file, preserved with
headings demoted one level (`##` to `###`) from an earlier single-file
AGENTS.md, or split out of AGENTS.md's "Invariants this package enforces"
section (the normative rule sentence stays in AGENTS.md; the
No-Regression/Observability evidence for it lives here). Continue in
[AGENTS-evidence-history-2.md](AGENTS-evidence-history-2.md) (issues #3492
through #5816),
[AGENTS-evidence-history-3.md](AGENTS-evidence-history-3.md) (issues #5761
and #5764), and
[AGENTS-evidence-history-4.md](AGENTS-evidence-history-4.md) (the #5764
round-6 and round-7 review follow-ups).

## Evidence

### Repository tenant-isolation canary evidence (#2048)

No-Regression Evidence: `go test ./internal/query -run
'Test(RepositoryList.*ScopedAuth|ResolveRepositorySelector.*ScopedAuth|ResolveRepositorySelectorDenies|RepositoryListSharedAuth|RepositoryListAllScopeAdmin)'
-count=1`. No-Observability-Change: the canary adds no route, graph write,
metric, label, runtime knob, or response field; existing repository query
spans, `repository_query.stage_*` logs, result limits, partial reasons, and
truncation metadata diagnose the path.

### Code search scoped-token route evidence (#2062)

No-Regression Evidence: `go test ./internal/query -run
'Test(CodeSearch|AuthMiddlewareWithScopedTokensAllowsCodeSearch)' -count=1` and
`go test ./internal/mcp -run
TestDispatchToolFindCodeAllowsScopedCodeSearchRoute -count=1`.
No-Observability-Change: the route adds no graph write, metric label, runtime
knob, or response field; existing code search truth envelopes, graph query
spans, HTTP route attribution, and content-store Postgres spans diagnose the
bounded read path.

### Entity resolution scoped-token route evidence (#2064)

No-Regression Evidence: `go test ./internal/query -run
'Test(ResolveEntity.*Scoped|ResolveEntity.*Grant|ResolveEntity.*AllScope|AuthMiddlewareWithScopedTokensAllowsEntityResolve)'
-count=1` and `go test ./internal/mcp -run
TestDispatchToolResolveEntityAllowsScopedEntityResolveRoute -count=1`.
No-Observability-Change: the route adds no graph write, metric label, runtime
knob, or response field; existing entity resolution truth envelopes, graph
query spans, HTTP route attribution, and content-store Postgres spans diagnose
the bounded read path.

### Content scoped-token route evidence (#2066)

No-Regression Evidence: `go test ./internal/query -run
'Test(ContentHandlerScoped|ContentHandlerAllScope|AuthMiddlewareWithScopedTokensAllowsContentRoutes)'
-count=1` and `go test ./internal/mcp -run
TestDispatchToolSearchFileContentAllowsScopedContentSearchRoute -count=1`.
No-Observability-Change: the route family adds no graph write, metric label,
runtime knob, or response field; existing content-store `postgres.query` spans
with `db.operation=get_file_content`, `get_file_lines`, `get_entity_content`,
`search_file_content`, and `search_entity_content`, plus HTTP route attribution
and truth envelopes, diagnose the bounded read path.

### Evidence citation scoped-token route evidence (#2068)

No-Regression Evidence: `go test ./internal/query -run
'Test(EvidenceHandler.*Citation.*(Scoped|AllScope)|AuthMiddlewareWithScopedTokensAllowsEvidenceCitationRoute)'
-count=1` and `go test ./internal/mcp -run
TestDispatchToolEvidenceCitationAllowsScopedCitationRoute -count=1`.
No-Observability-Change: the route adds no graph write, metric label, runtime
knob, or response field; existing `query.evidence_citation_packet` handler
spans, content-store `postgres.query` spans, HTTP route attribution, and truth
envelopes diagnose the bounded citation hydration path.

### Entity context scoped-token route evidence (#2070)

No-Regression Evidence: `go test ./internal/query -run
'Test(GetEntityContext.*Scoped|GetEntityContext.*Grant|GetEntityContext.*Fallback|AuthMiddlewareWithScopedTokensAllowsEntityContextRoute)'
-count=1` and `go test ./internal/mcp -run
TestDispatchToolEntityContextAllowsScopedEntityContextRoute -count=1`.
No-Observability-Change: the route adds no graph write, metric label, runtime
knob, or response field; existing entity context truth envelopes, graph query
spans, HTTP route attribution, and content-store Postgres spans diagnose the
bounded read path.

### Service/workload context scoped-token route evidence (#2072)

No-Regression Evidence: `go test ./internal/query -run
'Test(GetWorkload|GetService|ServiceWorkload|AuthMiddlewareWithScopedTokens)'
-count=1` and `go test ./internal/mcp -run
'TestDispatchTool(Service|Workload|ServiceAndWorkload)|TestEveryRegisteredToolHasDispatchRoute'
-count=1`. No-Observability-Change: the route family adds no graph write,
metric label, runtime knob, or response field; existing service query
`service_query.stage_*` logs, graph query spans, HTTP route attribution, truth
envelopes, result limits, and partial reasons diagnose the bounded read path.

### Service investigation scoped-token route evidence (#2074)

No-Regression Evidence: `go test ./internal/query -run
'Test(AuthMiddlewareWithScopedTokensAllowsServiceInvestigationRoute|InvestigateService)'
-count=1` and `go test ./internal/mcp -run
TestDispatchToolInvestigateServiceAllowsScopedRoute -count=1`.
No-Observability-Change: the route adds no graph write, metric label, runtime
knob, or response field; existing service query `service_query.stage_*` logs,
graph query spans, HTTP route attribution, truth envelopes, result limits, and
partial reasons diagnose the bounded investigation read path.

### Query playbook scoped-token route evidence (#2076)

No-Regression Evidence: `go test ./internal/query -run
'Test(QueryPlaybookHandler|AuthMiddlewareWithScopedTokensAllowsQueryPlaybookRoutes)'
-count=1` and `go test ./internal/mcp -run
'Test(QueryPlaybook|TestDispatchToolQueryPlaybooksAllowsScopedRoutes)'
-count=1`. No-Observability-Change: the route family adds no graph write, graph
read, Postgres read, metric label, runtime knob, or response field; existing
HTTP route attribution and query-playbooks truth envelopes diagnose the static
catalog/resolver path.

### Vulnerability scanner contract scoped-token route evidence (#2078)

No-Regression Evidence: `go test ./internal/query -run
'Test(VulnerabilityScannerReadContract|AuthMiddlewareWithScopedTokensAllowsScannerContractRoute)'
-count=1` and `go test ./internal/mcp -run
'Test(ResolveRouteMapsVulnerabilityScannerContract|DispatchToolScannerContractAllowsScopedRoute)'
-count=1`. No-Observability-Change: the route adds no graph write, graph read,
Postgres read, provider call, collector call, metric label, runtime knob, or
response field; existing HTTP route attribution and scanner-contract truth
envelopes diagnose the static route.

### Hosted governance status scoped-token route evidence (#2080)

No-Regression Evidence: `go test ./internal/query -run
'Test(StatusHandlerGovernance|GovernanceStatus|AuthMiddlewareWithScopedTokensAllowsGovernanceStatusRoute)'
-count=1` and `go test ./internal/mcp -run
'TestHostedGovernanceRuntimeToolRoutesToStatus|TestDispatchToolGovernanceStatusAllowsScopedRoute'
-count=1`. No-Observability-Change: the route adds no graph write, graph read,
content read, provider call, collector call, metric label, runtime knob, or
response field; existing HTTP route attribution and governance status truth
envelopes diagnose the redacted runtime readback path.

### Semantic extraction status scoped-token route evidence (#2082)

No-Regression Evidence: `go test ./internal/query -run
'Test(StatusHandlerSemanticExtraction|AuthMiddlewareWithScopedTokensAllowsSemanticExtractionStatusRoute)'
-count=1` and `go test ./internal/mcp -run
'TestSemanticCapabilityRuntimeToolRoutesToStatus|TestDispatchToolSemanticExtractionStatusAllowsScopedRoute'
-count=1`. No-Observability-Change: the route adds no graph write, graph read,
content read, provider call, collector call, metric label, runtime knob, or
response field; existing HTTP route attribution and semantic extraction status
truth envelopes diagnose the redacted runtime readback path.

### Component extension scoped-token route evidence (#2084)

No-Regression Evidence: `go test ./internal/query -run
'Test(ComponentExtensionsHandler|AuthMiddlewareWithScopedTokensAllowsComponentExtensionRoutes)'
-count=1` and `go test ./internal/mcp -run
'Test(ComponentExtensionToolsResolveToQueryRoutes|TestDispatchToolComponentExtensionsAllowsScopedRoutes)'
-count=1`. No-Observability-Change: the route adds no graph write, graph read,
content read, provider call, collector call, metric label, runtime knob, or
response field; existing HTTP route attribution and component-extension truth
envelopes diagnose the bounded local registry readback path.

### Hosted readiness scoped-token route evidence (#2090)

No-Regression Evidence: `go test ./internal/query -run
'Test(AuthMiddlewareWithScopedTokensAllowsHostedReadinessRoute|StatusHandlerHostedReadiness)'
-count=1` and `go test ./internal/mcp -run
'TestDispatchToolHostedReadinessAllowsScopedRoute' -count=1`.
No-Observability-Change: the route adds no graph write, content read, provider
call, collector call, metric label, runtime knob, or response field for
shared-token callers; existing HTTP route attribution and hosted readiness
status fields diagnose the bounded status readback path.

### Collector status scoped-token route evidence (#2088)

No-Regression Evidence: `go test ./internal/query -run
'Test(AuthMiddlewareWithScopedTokensAllowsCollectorStatusRoute|StatusHandler)'
-count=1` and `go test ./internal/mcp -run
'Test(ListCollectorsRuntimeToolRoutesToStatusCollectors|DispatchToolCollectorStatusAllowsScopedRoute)'
-count=1`. No-Observability-Change: the route adds no graph write, graph read,
content read, provider call, collector call, metric label, runtime knob, or
response field for shared-token callers; existing HTTP route attribution and
runtime status fields diagnose the bounded status readback path.

### Ingester status scoped-token route evidence (#2086)

No-Regression Evidence: `go test ./internal/query -run
'Test(AuthMiddlewareWithScopedTokensAllowsIngesterStatusRoutes|StatusHandler)'
-count=1` and `go test ./internal/mcp -run
'Test(ListIngestersRuntimeToolRoutesToStatusIngesters|GetIngesterStatusRuntimeToolRoutesToRepositoryStatus|DispatchToolIngesterStatusAllowsScopedRoutes)'
-count=1`. No-Observability-Change: the route adds no graph write, graph read,
content read, provider call, collector call, metric label, runtime knob, or
response field for shared-token callers; existing HTTP route attribution and
runtime status fields diagnose the bounded status readback path.

### Semantic evidence scoped-token route evidence (#2110)

No-Regression Evidence: `go test ./internal/query -run
'Test(AuthMiddlewareWithScopedTokensAllowsSemanticEvidenceRoutes|SemanticEvidenceHandlerScopedEmptyGrantReturnsEmptyWithoutRead|BuildSemanticEvidenceSQL.*Semantic)'
-count=1` and `go test ./internal/mcp -run
'Test(DispatchToolSemanticEvidenceAllowsScopedRoutes|SemanticEvidenceToolsRouteToBoundedHTTPReads)'
-count=1`.

### Edge resolution provenance surfacing (#2225)

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

### Registry bundle search targets the package registry catalog (#3493)

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

### Registry bundle search requires a scope (#3506 follow-up)

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

### Registry bundle scope validation rides the envelope (#3520 follow-up)

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

### Incident-context typed decode (#4794 W2a) and work-item evidence pagination fix (#4733)

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
