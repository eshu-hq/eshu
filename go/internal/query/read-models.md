# Query Read Models

This guide keeps route-specific read-model contracts close to `internal/query`
without making the package README carry every durable detail.

## Bounded graph and Postgres read models

`ImpactHandler` entity-map reads resolve a single typed anchor before graph
traversal and then use relationship-family Cypher shapes. The default
`depth=1` path emits a fixed set of direct one-hop
`MATCH (start)-[rel:TYPE]->(entity)` and
`MATCH (start)<-[rel:TYPE]-(entity)` reads rather than
`MATCH path = ... *1..1` variable traversals. Repository anchors focus the
default map on incoming deploys-from and config-reading consumers without
expanding structural `CONTAINS` / `REPO_CONTAINS`, outgoing repository, or
code-edge `CALLS` / `IMPORTS` families first. Explicit `relationship` filters
still use the requested relationship type, including structural, outgoing, or
code-edge families when an operator asks for them.
No-Regression Evidence: issue #503 characterized the previous hosted positive
repository-anchor path as `context deadline exceeded` while no-match controls
returned quickly. The regression coverage
`go test ./internal/query -run 'TestEntityMap' -count=1` now asserts repository
anchors use direct typed relationship-family traversal, keep explicit
`relationship=CONTAINS` support, backfill the requested type when a backend
omits scalar relationship-type projections, avoid untyped one-hop fanout,
preserve typed anchors, and keep deterministic `limit`/`truncated` response
coverage.
Performance Evidence: NornicDB source review classified the replacement shape
against `docs/performance/hot-path-query-cookbook.md` section 6.5
relationship-match start-node pruning. The affected stage is the graph-backed
entity-map API/MCP read path, expected start cardinality is one resolved entity
anchor, and the relationship-family fanout is a fixed list of known map
relationships rather than data-dependent structural expansion. The bounded proof
ladder is focused query tests, then hosted Compose API/CLI positive
repository-anchor timing before EKS rollout. The stop threshold is any positive
repository-anchor call still exceeding the normal CLI timeout or returning a
different truth envelope from the same graph.
No-Observability-Change: the route already emits `query.entity_map`, graph
query spans, truth metadata, `coverage.query_shape`, `coverage.depth`,
`coverage.limit`, relationship counts, relationship filters, and truncation;
the new shape keeps those operator-visible fields and updates
`coverage.query_shape` to identify the direct relationship-family path.
No-Regression Evidence: `go test ./internal/query -run
'TestEntityMapPopulatesTypedVerbAndEntityIDForVarLengthEdge' -count=1` covers
the NornicDB-compatible variable-length traversal row shape from issue #1604:
one resolved workload anchor, one incoming `DEFINES` relationship family, an
empty backend-derived `relationship_types` list, and a backend-reported
`length(path)=0`. The read model now emits the relationship verb as the
relationship-family literal, avoids `RETURN DISTINCT`, deduplicates equivalent
rows in Go after the bounded per-family graph calls, and reports a minimum
one-hop depth so the API/MCP entity map does not label the neighboring
repository as the anchor itself.
No-Observability-Change: the fix does not add a route, queue, worker, graph
write, runtime knob, metric instrument, or metric label. Operators continue to
diagnose entity-map reads through the existing `query.entity_map` handler span,
graph query spans, HTTP status/error body, truth envelope, `coverage.depth`,
`coverage.limit`, relationship filters, returned relationship counts, and
truncation metadata.
`PackageRegistryHandler` (`package_registry.go:21`) keeps package-registry
reads bounded: package and version identity lookups require a package,
ecosystem, or version anchor, dependency lookup requires `package_id` or
`version_id` plus `limit`, and correlation lookup requires `package_id` or
`repository_id` plus `limit`. Dependency routes return package-native
dependency truth only; correlation routes expose reducer-owned ownership
candidates, provenance-only publication evidence, and admitted manifest-backed
consumption without letting package source hints become ownership truth.
Package, version, and dependency reads return normalized identity fields
(`purl`, `bom_ref`, package manager, and source-debug fields) from already
materialized graph properties; they do not add whole-graph scans or source
payload re-parsing.
Package dependency reads start from `PackageDependency.package_id` or
`PackageDependency.version_id` before traversing to target packages, so sparse
packages with many versions but no dependency rows return an empty bounded page
instead of expanding every version first. A route-local read timeout caps the
graph call for API and MCP callers.
No-Regression Evidence: remote all-collector Compose proof on NornicDB
`nornicdb-pr177-search-index-flags:80719f25520e` showed the package-list
route returning `package_id:""` when the Cypher used `WITH p, count(v)`.
The diagnostics route proved the same package anchor returned scalar aliases
correctly when the package-list query used direct `RETURN ... count(v)` without
the intermediate `WITH`; focused coverage is
`go test ./internal/query -run TestPackageRegistryListPackagesUsesIndexedPackageScopeAndTruncates -count=1`.
No-Regression Evidence: PR #549 remote Compose proof returned
`count=0,truncated=false` for lodash package dependencies through both API and
MCP after package/version graph projection was fixed. The deterministic sparse
read regression is
`go test ./internal/query -run TestPackageRegistryListDependenciesReturnsEmptySparsePackageQuickly -count=1`,
covering a package anchor with no dependency rows, a server-side graph-read
deadline, and no package-version expansion before dependency discovery.
No-Regression Evidence: the follow-up branch proof against the existing
full-corpus remote Compose stack applied the two `PackageDependency` indexes to
NornicDB, started branch-built API and MCP processes on isolated host ports, and
queried lodash dependencies through both surfaces. API returned
`count=0`, `truncated=false`, `limit=5` in `7ms`; MCP returned
`Returned 0 result(s).` in `7ms`.
No-Observability-Change: the package registry handler already wraps the route
with `query.package_registry_packages`,
`query.package_registry_dependencies`, GraphQuery spans, HTTP status/errors,
truth envelope metadata, and response `count/limit/truncated` fields. Empty
dependency data is distinguishable from a slow or failed graph path by the
successful HTTP status, exact truth envelope, and `count=0,truncated=false`
payload.
No-Regression Evidence: `go test ./internal/query -run 'TestPackageRegistryList(PackagesUsesIndexedPackageScopeAndTruncates|VersionsUsesPackageUIDAnchor|DependenciesUsesPackageOrVersionAnchor)' -count=1`
proves package-registry reads return the new identity fields from bounded
package, version, and dependency query shapes.
No-Observability-Change: the read handlers use the same request spans,
GraphQuery spans, HTTP status/error path, truth envelope, limit, count, and
truncated fields; added identity properties do not create new worker lanes,
queues, or graph traversal paths.
Repository-scoped package-registry, CI/CD, service-catalog, and impact-explain
routes resolve human source repository selectors through the shared repository
catalog resolver before they call reducer-owned read models. The resolver
accepts canonical ids, repository names, repo slugs, indexed paths, local paths,
and remote URLs; unknown selectors return `404`, ambiguous selectors return
`400`, and container-image repository ids stay in the OCI/image identity domain
instead of being source-repository resolved.

No-Regression Evidence: `go test ./internal/query -run
'Test(SupplyChainSecurityAlertReconciliationsResolveNameOnlyCatalogProviderScope|SupplyChainSecurityAlertReconciliationsRejectAmbiguousNameOnlyProviderScope|SupplyChainSecurityAlertAggregateRoutesResolveNameOnlyCatalogProviderScope|PackageRegistryCorrelationsResolveRepositorySelectors|PackageRegistryCorrelationsRejectUnknownRepositorySelector|CICDRunCorrelationsResolveRepositorySelectors|CICDRunCorrelationAggregatesResolveRepositorySelectors|ServiceCatalogCorrelationsResolveRepositorySelectors|SupplyChainImpactExplainResolvesRepositorySelectors)'
-count=1` proves selector resolution for source repository names across
repository-scoped read models, exact provider-security-alert scope lookup for
name-only catalog rows, and fail-closed behavior for unknown or ambiguous
provider alert scopes.

No-Observability-Change: selector resolution runs before the existing bounded
Postgres read-model calls and content-catalog lookup is the only added common
path. It adds no graph write, queue, worker, reducer lane, runtime knob, metric
instrument, or metric label; operators still diagnose these routes through the
existing query spans, Postgres query duration metrics, truth envelopes, and
HTTP status/error bodies.
`CICDHandler` (`ci_cd.go:16`) reads reducer-owned CI/CD run correlation facts
from Postgres. It requires an explicit scope, repository, commit, provider-run,
artifact-digest, image-reference, or environment anchor plus `limit`, and it
keeps CI success, environment observations, and shell-only hints separate from
deployment truth. Repository-scoped list responses include `evidence_summary`
from the content read model so indexed GitHub Actions workflow files remain
visible when no live reducer run-correlation rows exist; static workflow
artifacts are explanatory evidence, not synthetic correlation rows. Static
workflow summaries also count explicit workflow image refs, unresolved templated
image commands, and ambiguous multi-image commands without returning raw shell
commands. The
`run_artifact_evidence` summary is computed only from the returned reducer page:
exact or derived artifact digests and image references are bridge evidence,
ambiguous artifact outcomes stay ambiguous, and provider-only runs report the
artifact/image bridge as missing instead of manufacturing deployment truth.
`ServiceCatalogHandler` (`service_catalog.go:16`) reads reducer-owned service
catalog ownership and drift correlation facts from Postgres. It requires an
explicit scope, entity, repository, service, workload, or owner anchor plus
`limit`, and it keeps catalog declarations provenance-only until reducer
evidence corroborates repository, service, workload, ownership, or drift truth.
`KubernetesHandler` (`kubernetes.go:16`) reads reducer-owned Kubernetes workload
correlation facts (`reducer_kubernetes_correlation`, produced by issue #388 PR1)
from Postgres. It requires an explicit scope, cluster, workload object,
namespace, image reference, or source digest anchor plus `limit`, exposes the
six-outcome contract (`exact`, `derived`, `ambiguous`, `unresolved`, `stale`,
`rejected`) and the `drift_kind` classification, and keeps a live workload
provenance-only unless its image digest or owner edge resolved exactly. The
handler writes nothing and projects no graph edge: the gated canonical edge is a
later PR. Reads are wrapped by the `query.kubernetes_correlations` span and the
`kubernetes.correlations.list` capability.
`ObservabilityCoverageHandler` (`observability_coverage.go:16`) reads
reducer-owned observability coverage correlation facts from Postgres
(`reducer_observability_coverage_correlation`, produced by the issue #391 PR1
reducer, expanded by #1118 for Grafana-stack evidence classes). It answers
whether a monitored cloud resource or service has alarm, dashboard, scrape,
rule, log, or trace coverage versus which coverage gaps remain. It requires an
explicit scope, provider, coverage-signal, observability-object, target
resource, or target service anchor plus `limit`; `source_class` and
`resource_class` are filters over that anchored page. It surfaces the outcome
contract (`exact`, `derived`, `ambiguous`, `unresolved`, `stale`, `rejected`,
`drifted`, `permission_hidden`) plus source-class labels (`declared`,
`applied`, `observed`, or `mixed`) and keeps coverage strictly structural:
it surfaces correlation IDs, the resolved target, source classes, freshness,
and evidence fact IDs only, never a health assertion derived from telemetry
values.
`SupplyChainHandler` (`supply_chain.go:16`) reads reducer-owned SBOM and
attestation attachment facts from Postgres. It requires a subject digest,
document ID, or document digest plus `limit`, and it keeps attachment status,
parse status, and verification status as separate response fields so callers do
not mistake parsed component evidence for trusted vulnerability impact or
promote `ambiguous_subject` attestations into canonical image attachments.
The same handler exposes reducer-owned container image identities through a
separate Postgres read model. Container image identity reads require a digest,
image reference, repository, or outcome anchor plus `limit`, and they keep
`identity_strength`, source layers, and evidence fact IDs visible so callers can
inspect digest admission without turning weak or stale tag diagnostics into
deployment truth.
The same handler exposes source-only advisory evidence through a Postgres
read model over active `vulnerability.*` facts. Advisory evidence reads require
a CVE, advisory, package, repository, service, or workload anchor plus `limit`.
Repository, service, and workload scopes first select active reducer-owned
impact findings, then use those finding CVE/advisory/package anchors to read
advisory source facts. They group GHSA, CVE/NVD, OSV, GLAD, EPSS, KEV, CWE,
affected package ranges, fixed versions, affected products, references,
withdrawal state, and source disagreements under a canonical advisory identity
without emitting impact findings or inferring that any additional repository,
image, workload, or deployment is affected. Provider-alert-only rows are not
used as advisory-evidence seeds.
The same handler exposes supply-chain impact findings through a separate
Postgres read model. Impact reads require a CVE, package, repository, subject
digest, or status anchor plus `limit`, and keep CVSS, EPSS, KEV, reachability,
fixed-version state, and missing evidence as separate fields. Exact
repository-scoped service-catalog correlations stay visible in
`evidence_path`. Catalog entity refs are read-model anchors, but they do not
become `service_ids` unless reducer evidence supplies an explicit service id.
When catalog evidence lacks a service id, workload id, and catalog entity ref,
list and explain responses carry `service/workload catalog anchor missing`
rather than collapsing that into absent service-catalog evidence. The
explanation route reads exactly one
active reducer finding by `finding_id` or
advisory/CVE plus package, repository, or subject-digest scope, then hydrates
only the referenced `evidence_fact_ids` so callers can inspect advisory,
package/version, dependency-chain, manifest/SBOM/image/workload, freshness,
and missing-evidence context without a whole-graph explain call.
The same handler exposes provider security alert reconciliations through a
separate Postgres read model. Security alert reconciliation reads require a
repository, provider, package, CVE, or GHSA anchor plus `limit`; provider state
and reconciliation status only filter anchored pages. Rows keep provider alert
state under `provider_alert` and Eshu-owned impact state under `eshu_impact`.
Repository-scoped security-alert reads match both the canonical repository id
and the provider repository scope id, so `provider_only` rows remain visible as
explicit missing-evidence rows instead of disappearing from repository pages.
Impact responses also attach a `readiness` envelope built by
`BuildSupplyChainImpactReadiness` (`supply_chain_impact_readiness.go:121`) so a
zero-finding result is classified as `not_configured`, `target_incomplete`,
`evidence_incomplete`, `readiness_unavailable`, `ready_zero_findings`,
`ready_with_findings`, or `ambiguous_scope`. The envelope echoes the bounded
target scope, lists
per-family fact counts and `latest_observed_at` for `vulnerability.advisory`,
`vulnerability.exploitability`, `package.consumption`, `package.registry`,
`sbom.component`, `sbom.attestation`, and `container_image.identity`, and
returns the stable `missing_evidence` reasons `advisory_sources`,
`owned_packages`, `sbom_or_image_evidence`, `target_collection_incomplete`,
and `readiness_unavailable`. The envelope also carries
`unsupported_targets[]` for observed package-manager evidence outside the
supported exact-version matcher set. Swift and Pub are excluded from
unsupported ecosystem targets only for exact `Package.resolved` or
`pubspec.lock` evidence joined to OSV `SwiftURL` or Pub advisory facts;
branch-only, revision-only, local, path, or private-hosted pins remain missing
or unsupported evidence. The envelope also carries
`source_snapshots[]` with source, ecosystem, cache artifact version, snapshot
digest, cache update time, freshness, completion state, and bounded warning
fields from `vulnerability.source_snapshot` facts scoped by requested CVE,
package, repository-owned ecosystem, or image component ecosystem.
`PostgresSupplyChainImpactReadinessStore`
(`supply_chain_impact_readiness_postgres.go:18`) runs one bounded CTE per
response with seven anchored counts, source-state/source-snapshot roll-ups, and
unsupported-target aggregation. The readiness path never invents findings,
never duplicates reducer matching, and adds one Postgres round trip alongside
the existing impact read; observability stays on the existing
`query.supply_chain_impact_findings` span, the
`query.supply_chain_impact_explanation` span, and the
`eshu_dp_postgres_query_duration_seconds` histogram.

No-Regression Evidence: `go test ./internal/query -run
'TestSupplyChainListAdvisoryEvidence|TestPostgresAdvisoryEvidenceStore|TestBuildAdvisoryEvidenceRows|TestAdvisoryEvidenceQuery' -count=1`
and `go test ./internal/mcp -run
'TestResolveRouteMapsAdvisoryEvidenceToBoundedQuery|TestAdvisoryEvidenceToolSchemaAdvertisesRepositoryScope' -count=1`
prove bounded advisory evidence input, repository selector resolution,
reducer-impact-only advisory anchoring, active source-fact SQL, MCP
dispatch/schema parity, canonical CVE/GHSA/OSV/NVD identity grouping, EPSS/KEV
enrichment, CVSS v3/v4/CWE preservation, affected package/product evidence,
withdrawn status, fixed-range and severity disagreements, pagination, and
source-only response shape.

No-Observability-Change: the advisory evidence route reuses the
`query.advisory_evidence` request span with route and capability attributes. It
uses the active Postgres fact read model only; it adds no graph query, queue,
reducer lane, worker, metric instrument, metric label, or runtime deployment
knob.

The same handler exposes cheap-summary aggregates over the reducer-owned impact
findings through a separate Postgres aggregate read model
(`supply_chain_impact_aggregates.go`). `CountSupplyChainImpactFindings` answers
total / affected / not_affected / per-priority / per-severity questions over an
optional CVE, package, repository id or selector, subject-digest, or impact-status scope.
`SupplyChainImpactInventory` returns a paginated grouped count along one of the
dimensions `impact_status`, `priority_bucket`, `severity` (bucketed from CVSS
score), or `repository_id`. The aggregate path is the cheap-summary call shape
that replaces the page-and-iterate caller workflow for ecosystem-level totals
questions exposed by `list_supply_chain_impact_findings`. It re-uses the
existing partial indexes on `fact_records` for
`reducer_supply_chain_impact_finding` (status, priority bucket, CVE,
package + repository + subject digest); no new schema or graph migration is
needed.

No-Regression Evidence: `go test ./internal/query -run
'TestSupplyChainImpactAggregate|TestSupplyChainImpactInventoryGroupExpression|TestSupplyChainImpactAggregateRoutesResolveRepositorySelectors'
-count=1` proves: 503 envelope when the store is missing, totals envelope shape,
grouped inventory shape, truncation marker + `next_offset` on overflow,
rejection of unknown grouping dimensions, oversized limits, and negative offsets,
and that the dimension-to-SQL-expression map is a closed enum (so SQL
substitution stays parameter-safe).

Observability Evidence: the aggregate routes add the
`query.supply_chain_impact_aggregate` request span (registered in
`go/internal/telemetry/contract_supply_chain.go`) with route and capability
attributes. They re-use the existing `eshu_dp_postgres_query_duration_seconds`
histogram and add no new graph query, queue, reducer lane, worker, or metric
instrument.

No-Regression Evidence: `go test ./internal/query -run 'TestSupplyChainImpactCanonicalFindingKeySupportsRollingUpgrades|TestSupplyChainImpactFindingQueryUsesCanonicalFindingRows|TestSupplyChainImpactAggregateQueriesCountCanonicalFindings|TestSupplyChainExplainImpactQueryKeepsRollingUpgradeFindingIDStable|TestSupplyChainExplainImpactQueryUsesCanonicalFindingRows|TestSupplyChainImpactAggregatePriorityQueryQualifiesPayload' -count=1` proves list, count, inventory, and explain reads collapse active reducer rows to canonical logical findings before paging, grouping, or ambiguity checks. The canonical partition key is always derived from stable payload identity fields so legacy rows without reducer `finding_id` and newer rows with reducer `finding_id` collapse together during rolling upgrades. Public read IDs prefer reducer `finding_id` when present and fall back to the same stable payload key for older rows, so source-scope and generation-specific fact IDs do not inflate user-facing vulnerability counts or cursors.

No-Observability-Change: canonical impact-finding dedupe stays inside the existing bounded Postgres read models. It adds no route, graph query, queue, worker, runtime knob, metric instrument, or metric label; operators still diagnose list, count, inventory, and explain latency through the existing query spans and Postgres query duration metrics.

The supply-chain impact explain payload includes a semantic hop overlay for
repository, image, workload, service, and environment evidence. These hop rows
are shaped from the reducer finding and the already-loaded evidence fact
previews; they do not perform a new graph traversal. Present hops carry
referenced evidence fact ids when the preview payload exposes the corresponding
anchor, while missing hops reuse the reducer-owned missing-evidence reasons so
callers can distinguish repository-only, image-only, and explicitly deployed
paths.

No-Regression Evidence: `go test ./internal/query -run
'TestBuildSupplyChainImpactExplanationReturns(RuntimePathAndMissingHops|SemanticMissingHops)'
-count=1` failed before explain output carried stable repository/image/workload/service/environment
hop rows, then passed with deployed-image evidence marked present and
repository-only evidence preserving missing image, workload, service, and
environment hops.

No-Observability-Change: semantic explain hops are response shaping over the
existing bounded explanation row and evidence previews. The route, Postgres
queries, query spans, and `eshu_dp_postgres_query_duration_seconds` metrics are
unchanged.

No-Regression Evidence: `go test ./internal/query -run 'TestSupplyChainImpactAggregateRoutesUseListProfileDefaults|TestSupplyChainImpactAggregateRoutesComprehensiveProfileIncludesPossiblyAffected|TestSupplyChainImpactAggregateRoutesCanonicalAndNameSelectorsShareProfileSemantics|TestSupplyChainImpactAggregateRoutesKeepSuppressionSeparateFromProfile|TestSupplyChainImpactAggregateQueriesUseListProfileAndSuppressionPredicates|TestOpenAPISpecIncludesSupplyChainImpactAggregateProfileFilters' -count=1` proves count and inventory now apply the same default precise detection profile as the list route, include comprehensive-only `possibly_affected` rows when `profile=comprehensive` is requested, resolve repository name and canonical id selectors to the same aggregate scope, and keep suppression filters independent of profile semantics.

No-Observability-Change: aggregate profile and suppression parity only adds predicates to the existing bounded Postgres aggregate read model and echoes the selected profile in the existing HTTP response. It adds no route, graph query, queue, worker, runtime knob, metric instrument, or metric label; operators still diagnose aggregate latency through the `query.supply_chain_impact_aggregate` span and Postgres query duration metrics.

The same handler exposes cheap-summary aggregates over the reducer-owned
provider security alert reconciliations through a separate Postgres aggregate
read model (`security_alert_reconciliation_aggregates.go`).
`CountSecurityAlertReconciliations` answers total / per-reconciliation-status /
per-provider / per-provider-state questions over an optional repository id or
selector, provider, package, CVE, GHSA, provider-state, or
reconciliation-status scope.
`SecurityAlertReconciliationInventory` returns a paginated grouped count along
one of the dimensions `reconciliation_status`, `provider`, `provider_state`,
`repository_id`, or `package_id`. The aggregate replaces the page-and-iterate
caller workflow for ecosystem-level questions exposed by
`list_security_alert_reconciliations`. Repository-scoped aggregate reads use the
same canonical-plus-provider scope set as the list route, preserving
provider-only rows in counts and grouped inventory. It re-uses the existing
partial indexes
on `fact_records` for `reducer_security_alert_reconciliation`
(repository_id + package_id + reconciliation_status; provider_repository_id +
package_id + reconciliation_status; scope_id + package_id +
reconciliation_status; provider + provider_state + reconciliation_status;
cve_ids GIN; ghsa_ids GIN); no graph migration is needed.

No-Regression Evidence: `go test ./internal/query -run
'TestSecurityAlertReconciliationAggregate|TestSecurityAlertReconciliationInventoryGroupExpression|TestNextSecurityAlertReconciliationAggregateOffset|TestSupplyChainSecurityAlertAggregateRoutesResolveRepositorySelectors|TestSecurityAlertReconciliationAggregateSourceFreshnessUsesCurrentFactAlias'
-count=1` proves: 503 envelope when the store is missing, totals envelope shape
with the three rollup maps, grouped inventory shape, truncation marker plus
`next_offset` on overflow, rejection of unknown grouping dimensions, oversized
limits, negative offsets, and oversized offsets, null `next_offset` when the
next page would exceed the documented offset bound, and that the
dimension-to-SQL-expression map is a closed enum (so SQL substitution stays
parameter-safe). It also proves the source-freshness rollup uses the current
fact alias after the CTE and that repository selector filters include provider
repository scope ids.

Observability Evidence: the aggregate routes add the
`query.security_alert_reconciliation_aggregate` request span (registered in
`go/internal/telemetry/contract_supply_chain.go`) with route and capability
attributes. They re-use the existing `eshu_dp_postgres_query_duration_seconds`
histogram and add no new graph query, queue, reducer lane, worker, or metric
instrument.

The same handler exposes cheap-summary aggregates over the reducer-owned
container image identities through a separate Postgres aggregate read model
(`container_image_identity_aggregates.go`). `CountContainerImageIdentities`
answers total / per-outcome / per-identity-strength questions over an
optional digest, image_ref, repository_id, or outcome scope. When a
`source_repository_id` scope returns zero identities, the count response also
includes `source_bridge.missing_evidence` with the same public-safe bridge
classes exposed by the list route.
`ContainerImageIdentityInventory` returns a paginated grouped count along one
of the dimensions `outcome`, `identity_strength`, or `repository_id`. The
aggregate replaces the page-and-iterate caller workflow for ecosystem-level
questions like "how many images resolved by exact digest vs tag?" exposed by
`list_container_image_identities`. It re-uses the existing partial indexes on
`fact_records` for `reducer_container_image_identity` (digest, image_ref,
repository_id, outcome); no new schema or graph migration is needed.

No-Regression Evidence: `go test ./internal/query -run
'TestContainerImageIdentityAggregate|TestContainerImageIdentityInventoryGroupExpression|TestNextContainerImageIdentityAggregateOffset'
-count=1` proves: 503 envelope when the store is missing, totals envelope shape
with the two rollup maps, grouped inventory shape, truncation marker plus
`next_offset` on overflow, rejection of unknown grouping dimensions, oversized
limits, negative offsets, and oversized offsets, null `next_offset` when the
next page would exceed the documented offset bound, and that the
dimension-to-SQL-expression map is a closed enum.
`go test ./internal/query -run
'TestContainerImageIdentityAggregateCountReportsSourceBridgeMissingEvidence'
-count=1` proves source-scoped count zeroes keep the named bridge classes
instead of returning only an aggregate zero.

Observability Evidence: the aggregate routes add the
`query.container_image_identity_aggregate` request span (registered in
`go/internal/telemetry/contract_supply_chain.go`) with route and capability
attributes. They re-use the existing `eshu_dp_postgres_query_duration_seconds`
histogram and add no new graph query, queue, reducer lane, worker, or metric
instrument.

`CICDHandler` (`ci_cd.go`) also exposes cheap-summary aggregates over the
reducer-owned CI/CD run correlations through a separate Postgres aggregate
read model (`ci_cd_run_correlation_aggregates.go`). `CountCICDRunCorrelations`
answers total / per-outcome / per-environment / per-provider questions over
an optional scope, repository, commit, provider, artifact digest, image
reference, environment, or outcome scope. `CICDRunCorrelationInventory` returns a
paginated grouped count along one of the dimensions `outcome`, `environment`,
`repository_id`, or `provider`. The aggregate replaces the page-and-iterate
caller workflow for ecosystem-level questions like "how many runs ended in
each outcome per environment?" exposed by `list_ci_cd_run_correlations`. It
re-uses the existing partial indexes on `fact_records` for
`reducer_ci_cd_run_correlation`: the composite lookup index covers
repository_id + commit_sha + artifact_digest + environment + outcome, and
separate partial lookup indexes cover run/provider, commit_sha,
artifact_digest, image_ref, and environment; no new schema or graph migration
is needed. The aggregate handler validates the
`outcome` filter against the same enum the list endpoint advertises in
`openapi_paths_cicd.go` (`exact`, `derived`, `ambiguous`, `unresolved`,
`rejected`), so typos surface as 400 instead of silently returning zero
counts.

No-Regression Evidence: `go test ./internal/query -run
'TestCICDRunCorrelationAggregate|TestCICDRunCorrelationInventoryGroupExpression|TestNextCICDRunCorrelationAggregateOffset'
-count=1` proves: 503 envelope when the store is missing, totals envelope
shape with three rollup maps, grouped inventory shape, truncation marker
plus `next_offset` on overflow, rejection of unknown grouping dimensions,
unknown outcomes, oversized limits, negative offsets, and oversized offsets,
null `next_offset` at the offset ceiling, and that the
dimension-to-SQL-expression map is a closed enum.

Observability Evidence: the aggregate routes add the
`query.ci_cd_run_correlation_aggregate` request span (registered in
`go/internal/telemetry/contract_cicd.go`) with route and capability
attributes. They re-use the existing `eshu_dp_postgres_query_duration_seconds`
histogram and add no new graph query, queue, reducer lane, worker, or metric
instrument.

The observability coverage read surface (`observability_coverage.go`,
`observability_coverage_correlations.go`) mirrors this same bounded Postgres
read model for the issue #391 `reducer_observability_coverage_correlation`
facts: anchor-or-400, `limit`-required, deterministic `ORDER BY fact.fact_id
ASC` keyset paging on `fact_id`, and the active-generation join filtered to
`is_tombstone = FALSE` and `generation.status = 'active'`, so duplicate facts,
tombstoned (stale) coverage, and inactive generations never leak into a page.
The same page can be filtered by `source_class` and `resource_class` without
making either field a whole-store anchor.

No-Regression Evidence: `go test ./internal/query -run
'TestObservabilityCoverage|TestOpenAPISpecIncludesObservabilityCoverageCorrelations'
-count=1` proves: anchor-or-400 plus required `limit`, the bounded store call
with `limit+1` overfetch, `truncated` + `next_cursor.after_correlation_id`
keyset paging, the active-fact read-model predicates (fact kind, `is_tombstone
= FALSE`, `generation.status = 'active'`, payload anchors, and `fact_id`
cursor), source-class/resource-class filters, and OpenAPI wire-contract
lockstep. This is a read-only query slice over facts already on `main`; it adds
no graph write, worker, lease, or schema change.

Observability Evidence: the route adds the
`query.observability_coverage_correlations` request span (registered in
`go/internal/telemetry/contract_z_observability_coverage.go`) with route and
capability attributes and re-uses the existing
`eshu_dp_postgres_query_duration_seconds` histogram. No new metric instrument
is introduced; the `coverage_signal` metric dimension and
`eshu_dp_observability_coverage_correlations_total` counter were already added
by the issue #391 PR1 reducer slice.

`PackageRegistryHandler` (`package_registry.go`) exposes cheap-summary
aggregates over the graph (:Package) corpus through a separate graph-backed
aggregate read model (`package_registry_aggregates.go`). This is the first
**graph-backed** aggregate (previous aggregates ride on Postgres
`fact_records`); the Reader uses the `GraphQuery` port — the same interface
the existing list handler reads — so handler tests can inject an in-memory
stub. `CountPackageRegistryPackages` answers total + per-ecosystem questions
over an optional ecosystem / registry / namespace / package_manager /
visibility scope; `PackageRegistryPackageInventory` returns a paginated
grouped count along one of those five dimensions. The aggregate replaces
the page-and-iterate caller workflow for ecosystem-level questions exposed
by `list_package_registry_packages`.

Hot-path eligibility relies on a label-property anchor against an indexed
property, per the NornicDB-New hot-path cookbook Area 5 (grouped count) and
`PatternOutgoingCountAgg`. The existing `package_ecosystem` and
`package_normalized_name` indexes are joined in this PR by four new indexes
on `(:Package).registry`, `(:Package).namespace`, `(:Package).package_manager`,
and `(:Package).visibility` (`go/internal/graph/schema.go`). Operators
applying this PR must re-run `eshu-bootstrap-data-plane` so the new indexes
exist before the aggregate routes resolve in production; the routes return
correct counts without the indexes but fall back to a label scan instead of
the cookbook hot path.

No-Regression Evidence: `go test ./internal/query -run
'TestPackageRegistryAggregate|TestPackageRegistryInventoryGroupExpression|TestNextPackageRegistryAggregateOffset|TestGraphPackageRegistryAggregateStore'
-count=1` proves: 503 envelope when the store is missing, totals envelope
shape with the per-ecosystem rollup, grouped inventory shape, truncation
marker plus `next_offset` on overflow, rejection of unknown grouping
dimensions, unknown visibility, oversized limits, negative offsets, and
oversized offsets, null `next_offset` at the offset ceiling, the
closed-enum dimension map, AND that the production Reader emits Cypher
with the cookbook hot-path shape (label-property anchor `MATCH (p:Package)`
plus indexed-property predicate plus `ORDER BY bucket_count DESC` plus
parameter-bound limit). A `PROFILE` proof against the pinned NornicDB
binary is the operator gate for this PR: the in-process tests prove the
Cypher shape stays hot-path-eligible, but the operator running
`eshu-bootstrap-data-plane` must capture `PROFILE` output for the four new
indexes before promoting the routes in production.

Observability Evidence: the aggregate routes add the
`query.package_registry_aggregate` request span (registered in
`go/internal/telemetry/contract_package_registry.go`) with route and
capability attributes. They re-use the existing query-handler tracing and
the `neo4j.query` graph span; no new metric instrument is added.

`SupplyChainHandler` (`supply_chain.go`) also exposes cheap-summary
aggregates over the reducer-owned SBOM and attestation attachments through a
separate Postgres aggregate read model
(`sbom_attestation_attachment_aggregates.go`).
`CountSBOMAttestationAttachments` answers total + per-attachment-status +
per-artifact-kind questions over an optional subject_digest, document_id,
document_digest, repository_id, workload_id, service_id, attachment_status, or
artifact_kind scope.
`SBOMAttestationAttachmentInventory` returns a paginated grouped count along
one of the dimensions `attachment_status`, `artifact_kind`, or
`subject_digest`. The aggregate replaces the page-and-iterate caller
workflow for ecosystem-level questions like "how many attestations are
verified vs unverified?" exposed by `list_sbom_attestation_attachments`. It
re-uses the existing partial indexes on `fact_records` for
`reducer_sbom_attestation_attachment` (subject_digest + attachment_status,
document_id, document_digest, attachment_status + artifact_kind); no new
schema or graph migration is needed. Source-anchor predicates use the
reducer-owned `repository_ids`, `workload_ids`, and `service_ids` payload arrays,
so a scoped request with no matching evidence returns scoped zero instead of
falling back to global attachment totals. The handler validates the
`attachment_status` and `artifact_kind` filters against the same closed
enums the list endpoint advertises in `openapi_paths_supply_chain_sbom.go`,
so typos surface as 400 instead of silently returning zero counts.

No-Regression Evidence: `go test ./internal/query -run
'Test(SupplyChainListSBOMAttestationAttachmentsAcceptsRepositoryScope|SBOMAttestationAttachmentAggregateRoutesForwardSourceScopes|SBOMAttestationAttachmentAggregateQueriesFilterSourceScopes|SBOMAttestationAttachmentAggregateRoutesDoNotDropServiceScope)'
-count=1` proves list/count/inventory source scopes are carried into the
bounded read model and echoed in response scope. This prevents service,
workload, or repository scoped aggregate requests from returning unscoped
global attachment totals.

No-Observability-Change: this read-surface change reuses the existing
`query.sbom_attestation_attachments` and
`query.sbom_attestation_attachment_aggregate` request spans plus Postgres query
duration instrumentation. It adds no worker, queue, graph write, metric
instrument, metric label, runtime flag, or deployment knob.

No-Regression Evidence: `go test ./internal/query -run
'TestSBOMAttestationAttachmentAggregate|TestSBOMAttestationAttachmentInventoryGroupExpression|TestNextSBOMAttestationAttachmentAggregateOffset'
-count=1` proves: 503 envelope when the store is missing, totals envelope
shape with the two rollup maps, grouped inventory shape, truncation marker
plus `next_offset` on overflow, rejection of out-of-contract
attachment_status / artifact_kind values, unknown grouping dimensions,
oversized limits, negative offsets, and oversized offsets, null
`next_offset` at the offset ceiling, and that the
dimension-to-SQL-expression map is a closed enum.

Observability Evidence: the aggregate routes add the
`query.sbom_attestation_attachment_aggregate` request span (registered in
`go/internal/telemetry/contract_supply_chain.go`) with route and capability
attributes. They re-use the existing `eshu_dp_postgres_query_duration_seconds`
histogram and add no new graph query, queue, reducer lane, worker, or
metric instrument.

`DocumentationHandler` (`documentation.go`) also exposes cheap-summary
aggregates over durable documentation finding facts through a separate
Postgres aggregate read model (`documentation_finding_aggregates.go`). The
aggregate scopes to `fact_kind = 'documentation_finding'` and applies
`viewer_can_read_source = true`, `source_acl_evaluated <> false`, and
`permission_decision <> denied` in SQL so grouped counts cannot disclose
protected findings. The list route intentionally differs: it retrieves those
rows and applies honest redaction and access disposition in Go rather than
silently presenting them as absent. Both routes separately enforce the
caller's repository and scope authorization. `CountDocumentationFindings`
answers total + per-status + per-truth-level + per-freshness-state
questions over an optional scope, finding_type, source_id, document_id,
status, truth_level, or freshness_state scope.
`DocumentationFindingInventory` returns a paginated grouped count along
one of the dimensions `status`, `truth_level`, `freshness_state`,
`finding_type`, or `source_id`. The aggregate replaces the
page-and-iterate caller workflow for ecosystem-level questions exposed by
`list_documentation_findings`. Migrations 064 and 065 replace the retired
ACL-filtered index with the broader findings read index. Its predicate matches
the shared `fact_kind` and tombstone boundary, while its ordered keys cover all
five grouping dimensions used here plus `document_id`.

The `/documentation/facts` free-text surface was evaluated in this issue. Valid
GIN candidates improved read latency, but the fastest option more than doubled
the exact production streaming-write cost. The unchanged 1.6-million-row local
search finished in 1.252 seconds, so no free-text index ships.

No-Regression Evidence: `go test ./internal/query -run
'TestDocumentationFindingAggregate|TestDocumentationFindingInventoryGroupExpression|TestNextDocumentationFindingAggregateOffset|TestDocumentationFindingAggregateSQLIncludesPermissionPredicates'
-count=1` proves: 503 envelope when the store is missing, totals envelope
shape with the three rollup maps, grouped inventory shape, truncation
marker plus `next_offset` on overflow, rejection of unknown grouping
dimensions, oversized limits, negative offsets, and oversized offsets,
null `next_offset` at the offset ceiling, the closed-enum dimension map,
AND that all three production SQL templates (total, group, inventory)
contain the three permission predicate substrings so a future refactor
cannot accidentally drop the access gate.

Observability Evidence: the aggregate routes add the
`query.documentation_aggregate` request span (registered in
`go/internal/telemetry/contract.go`) with route and capability
attributes. They re-use the existing `eshu_dp_postgres_query_duration_seconds`
histogram and add no new graph query, queue, reducer lane, worker, or
metric instrument.

## Runtime and investigation read models

Both backends instrument every query with an OTEL span (`neo4j.query`,
`postgres.query`). Handlers that span multiple read stages use
`startQueryHandlerSpan` (`handler_tracing.go:16`) with a stable span name from
`telemetry.SpanQuery*` constants to attach route and capability attributes.
Code topic and change-surface investigations wrap handlers in their
`telemetry.SpanQuery*` spans; content reads emit `postgres.query` spans with
scoped `db.operation` values.
Repository and service read paths additionally emit stage-start/stage-done log
events via `repositoryQueryStageTimer` and `serviceQueryStageTimer`.

The response is written with `WriteSuccess` when the caller sends
`Accept: application/eshu.envelope+json`; this wraps the payload in a
`ResponseEnvelope` containing `data`, `truth` (`TruthEnvelope`), and `error`
fields. Without that header, `WriteJSON` emits the legacy payload directly.
`BuildTruthEnvelope` (`contract.go:547`) constructs the `TruthEnvelope`; it
panics if the capability string is not in `capabilityMatrix`.
Repository runtime artifacts parse Dockerfile stage metadata through
`buildDockerfileRuntimeArtifacts`, including base image, base tag, build
platform, copy-from, command, port, and environment signals.
Deployment trace image references can be enriched with projected OCI registry
truth when `ContainerImage`, `ContainerImageIndex`, or
`ContainerImageDescriptor` graph rows exist. Digest references surface as
canonical image identity; mutable tag references surface only when a registry
tag observation resolves to one projected digest, and conflicting tag
observations stay ambiguous. Digest reads start from ContainerImage-family
`digest` anchors before joining repository metadata with
`repo.uid = image.repository_id` (`impact_trace_deployment_oci.go:70`), and tag
reads start from `ContainerImageTagObservation.image_ref`, join the repository
with `repo.uid = tag.repository_id`, then match the resolved digest image
(`impact_trace_deployment_oci.go:103`). The read helper runs one bounded query
per OCI image-family label instead of a `CALL { ... UNION ... }` subquery
followed by `MATCH`, because NornicDB v1.1.1 rejects that post-`CALL` shape.
This keeps deployment trace truth accurate while avoiding high-cost OCI
publication relationship traversals in the NornicDB-backed canonical write path.
Content-backed Argo CD relationship fallback reads `source_repos` for
multi-source Applications and emits one `DEPLOYS_FROM` relationship per source
repo while still accepting the older singular `source_repo` metadata field.
Code topic investigation is the coverage-first path for broad behavior prompts
such as repo sync authentication or workspace locking. It scores
`content_entities` and `content_files` in one bounded query, returns ranked
`repo_id + relative_path` evidence, matched symbols, coverage/truncation, and
follow-up handles for `get_file_lines` and `get_code_relationship_story`.
Structural inventory uses `content_entities` as the first-call read model for
function/class lists, top-level file elements, dataclasses, documented
functions, decorated methods, classes with a method, `super()` calls, and
function counts per file. `POST /api/v0/code/structure/inventory` keeps those
prompts out of raw Cypher by applying repo/path/language/type filters before a
deterministic `limit+1` page and returning source handles for drill-down reads.
Import dependency investigation uses the graph as the first-call read model for
imports by file, importers, package imports, direct Python file import cycles,
and cross-module calls. `POST /api/v0/code/imports/investigate` requires a
repository, file, or module scope anchor before expanding relationships,
returns deterministic `limit+1` pages with `truncated` and `next_offset`, and
includes source handles for follow-up file reads.
Call graph metrics use the graph as the first-call read model for recursive
function and hub-function prompts. `POST /api/v0/code/call-graph/metrics`
requires a repository scope before expanding `CALLS`, returns deterministic
`limit+1` pages with `truncated` and `next_offset`, and includes source handles,
hub call-degree counts, and recursion evidence for follow-up file reads.
The OpenAPI fragments for `POST /api/v0/code/dead-code` and
`POST /api/v0/code/dead-code/investigate` name modeled language roots such as
Go public-package exports plus C, C#, Dart, Haskell, Kotlin, Elixir, PHP, and
Groovy parser-backed roots. The investigation route reuses the same bounded
candidate scan but returns coverage, candidate buckets, source handles, and
recommended next calls for MCP clients. JavaScript and TypeScript candidates
stay ambiguous in investigation mode until corpus precision evidence proves
cleanup safety. Its language filter examples include `csharp`, `c`, `dart`,
`haskell`, `kotlin`, `elixir`, `php`, `groovy`, and `sql`; `csharp` is
normalized to `c_sharp` before candidate scanning.
