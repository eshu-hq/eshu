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
`PackageRegistryHandler` (`package/registry/handler.go`) keeps package-registry
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

Repository-scoped package-registry, CI/CD, service-catalog, and impact-explain
routes resolve human source repository selectors through the shared repository
catalog resolver before they call reducer-owned read models. The resolver
accepts canonical ids, repository names, repo slugs, indexed paths, local paths,
and remote URLs; unknown selectors return `404`, ambiguous selectors return
`400`, and container-image repository ids stay in the OCI/image identity domain
instead of being source-repository resolved.

`cicd.Handler` (`cicd/handler.go`) reads reducer-owned CI/CD run correlation facts
from Postgres. It requires an explicit scope, repository, commit, provider-run,
artifact-digest, image-reference, or environment anchor plus `limit`, and it
keeps CI success, environment observations, and shell-only hints separate from
deployment truth. Repository-scoped list responses include `evidence_summary`
from the content read model so indexed GitHub Actions workflow files remain
visible when no live reducer run-correlation rows exist; static workflow
artifacts are explanatory evidence, not synthetic correlation rows. Static
workflow summaries also count explicit workflow image refs, unresolved templated
image commands, and ambiguous multi-image commands without returning raw shell
commands. The `run_artifact_evidence` summary is computed only from the returned
reducer page: exact or derived artifact digests and image references are bridge
evidence, ambiguous artifact outcomes stay ambiguous, and provider-only runs
report the artifact/image bridge as missing instead of manufacturing deployment
truth.

`CatalogHandler` (`service/catalog.go:22`) reads reducer-owned service
catalog ownership and drift correlation facts from Postgres. It requires an
explicit scope, entity, repository, service, workload, or owner anchor plus
`limit`, and it keeps catalog declarations provenance-only until reducer
evidence corroborates repository, service, workload, ownership, or drift truth.
`kubernetes.Handler` (`kubernetes/handler.go`) reads reducer-owned Kubernetes workload
correlation facts (`reducer_kubernetes_correlation`, produced by issue #388 PR1)
from Postgres. It requires an explicit scope, cluster, workload object,
namespace, image reference, or source digest anchor plus `limit`, exposes the
six-outcome contract (`exact`, `derived`, `ambiguous`, `unresolved`, `stale`,
`rejected`) and the `drift_kind` classification, and keeps a live workload
provenance-only unless its image digest or owner edge resolved exactly. The
handler writes nothing and projects no graph edge: the gated canonical edge is a
later PR. Reads are wrapped by the `query.kubernetes_correlations` span and the
`kubernetes.correlations.list` capability.
`coverage.Handler` (`observability/coverage/handler.go`) reads
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
`SupplyChainHandler` (`handler.go:16`) reads reducer-owned SBOM and
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
deployment truth. The read model is canonical by digest: current support sets
from every active scope fold to one logical identity and a digest-derived stable
cursor. Evidence IDs and correlation arrays are sorted unions across those
supports, while identity strength is selected independently so an explicit
runtime digest observation remains visible when a CI artifact corroborates the
same digest. Scoped-token grants filter supports before that fold, so the returned
repository, workload, service, and evidence arrays contain only associations
the caller is allowed to observe. Activation clears a scope's current support
pointer before its reducer refresh completes; stale generations therefore stop
contributing immediately instead of remaining visible until a later write.
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
and missing-evidence context without a whole-graph explain call. With `ESHU_SUPPLY_CHAIN_IMPACT_WINNERS_READ` on, a scoped-token explain and any public `finding_id` lookup read `supply_chain_impact_finding_supports` (#10880 PR-B2a) and answer 503 while it is not ready; the advisory evidence anchors for a repository, service, or workload call come from it too.
The same handler exposes provider security alert reconciliations through a
separate Postgres read model. Security alert reconciliation reads require a
repository, provider, package, CVE, or GHSA anchor plus `limit`; provider state
and reconciliation status only filter anchored pages. Rows keep provider alert
state under `provider_alert` and Eshu-owned impact state under `eshu_impact`.
Repository-scoped security-alert reads match both the canonical repository id
and the provider repository scope id, so `provider_only` rows remain visible as
explicit missing-evidence rows instead of disappearing from repository pages.
Impact responses also attach a `readiness` envelope built by
`BuildSupplyChainImpactReadiness` (`readiness.go:121`) so a
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
(`readiness_postgres.go:18`) runs one bounded CTE per
response with seven anchored counts, source-state/source-snapshot roll-ups, and
unsupported-target aggregation. The readiness path never invents findings,
never duplicates reducer matching, and adds one Postgres round trip alongside
the existing impact read; observability stays on the existing
`query.supply_chain_impact_findings` span, the
`query.supply_chain_impact_explanation` span, and the
`eshu_dp_postgres_query_duration_seconds` histogram.

The same handler exposes cheap-summary aggregates over the reducer-owned impact
findings through a separate Postgres aggregate read model
(`aggregates.go`). `CountSupplyChainImpactFindings` answers
total / affected / not_affected / per-priority / per-severity questions over an
optional CVE, package, repository id or selector, subject-digest, or impact-status scope.
`SupplyChainImpactInventory` returns a paginated grouped count along one of the
dimensions `impact_status`, `priority_bucket`, `severity` (bucketed from CVSS
score), or `repository_id`. The aggregate path is the cheap-summary call shape
that replaces the page-and-iterate caller workflow for ecosystem-level totals
questions exposed by `list_supply_chain_impact_findings`. It re-uses the
existing partial indexes on `fact_records` for
`reducer_supply_chain_impact_finding` (status, priority bucket, CVE,
package + repository + subject digest). With the winners gate on, a scoped-token count or inventory reads `supply_chain_impact_finding_supports` instead (#10880 PR-B2a): grant first through the scope and repository indexes, canonical copy picked among the granted copies, rank filters after, 503 while the table is not ready, no winners or fact fallback.

Workload, service, and environment filters accept only a current active
repository mapping resolved at read time. Reducer-baked arrays remain evidence
on the finding payload but never satisfy these selectors because they become
stale after a redeploy, retraction, or promotion. The shared SQL CTE narrows
runtime facts by the requested dimension before it
decodes repository anchors: `reducer_workload_identity` and accepted
`reducer_service_catalog_correlation` facts supply workload/service mappings,
and accepted `reducer_ci_cd_run_correlation` facts supply environments.
Tombstoned, stale-generation, ambiguous, rejected, unresolved, and
provenance-only correlations do not match. Two partial workload-identity
indexes cover scalar `workload_id` and `entity_keys` membership so the read
does not probe every active ingestion scope. Before a decoded runtime mapping
can affect membership, it is intersected with the same allowed-repository or
allowed-scope grant used by the finding query; selectors cannot act as a
cross-scope correlation oracle. Response-side `runtime_context` hydration
applies that identical per-fact grant before folding workloads, services,
deployments, environments, or catalog refs, so a scoped response never
advertises context that its corresponding filter must reject.
Finding-bound environment corroboration additionally batches at most 200
visible `(subject_digest, environment)` candidate occurrences across the final
page. Round-robin allocation gives every eligible finding at least one slot;
identical pairs share one SQL lookup. A candidate enters `runtime_context` only
when a current active authorized accepted `reducer_ci_cd_run_correlation`
matches both values. This mirrors the reducer's strong digest deployment branch
across builder/deployer repository seams and is artifact deployment context,
not repository ownership. The baked environment name is only a selector; the
evidence state is recomputed from current facts, with `deploy_event` winning
over `declared`. Repository mappings contribute candidate names only; an
unconfirmed name receives no evidence entry. Per-row
`environment_evidence_probe` reports the checked quota and whether visible
candidate names were truncated, so serialized evidence cannot exceed the same
200-occurrence page budget. It does not discover environments absent from the
finding.
Direct identity fields are accepted only when their JSON value is a string.
Objects, arrays, numbers, and booleans fail closed in both SQL membership and
Go hydration; malformed direct repository and scope anchors fall through to
the next valid canonical anchor instead of becoming formatted identities.

The supply-chain impact explain payload includes a semantic hop overlay for
repository, image, workload, service, and environment evidence. These hop rows
are shaped from the reducer finding and the already-loaded evidence fact
previews; they do not perform a new graph traversal. Present hops carry
referenced evidence fact ids when the preview payload exposes the corresponding
anchor, while missing hops reuse the reducer-owned missing-evidence reasons so
callers can distinguish repository-only, image-only, and explicitly deployed
paths.

The same handler exposes cheap-summary aggregates over the reducer-owned
provider security alert reconciliations through a separate Postgres aggregate
read model (`supply/chain/alerts/aggregates.go`).
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
`list_container_image_identities`. Totals, outcome buckets, and identity-strength
buckets count one canonical digest after authorization and filtering.
Repository inventory counts each visible digest once per repository support;
because one digest can legitimately be supported by multiple repositories,
repository buckets are deliberately non-additive. These queries use the typed
`container_image_identity_current_supports` projection and its set-first
digest/ref/repository/outcome indexes rather than JSON predicates over
`fact_records`.

The public digest aggregate is intentionally not the reducer evidence seam.
Supply-chain impact, CI/CD correlation, and SBOM attachment load bounded rows
from `container_image_identity_current_support_facts_for`, which preserves one
support's correlated image, registry repository, source repository, build
provenance, and runtime fields. Those reducers keep their existing domain
selection and ambiguity rules; only list, aggregate, authorization, and source
bridge presentation folds supports to one logical digest identity.

`cicd.Handler` (`cicd/handler.go`) also exposes cheap-summary aggregates over the
reducer-owned CI/CD run correlations through a separate Postgres aggregate
read model (`cicd/run_correlation_aggregates.go`). `CountRunCorrelations`
answers total / per-outcome / per-environment / per-provider questions over
an optional scope, repository, commit, provider, artifact digest, image
reference, environment, or outcome scope. `RunCorrelationInventory` returns a
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
`openapi/paths/cicd/routes.go` (`exact`, `derived`, `ambiguous`, `unresolved`,
`rejected`), so typos surface as 400 instead of silently returning zero
counts.

The observability coverage read surface (`observability/coverage/handler.go`,
`observability/coverage/correlations.go`) mirrors this same bounded Postgres
read model for the issue #391 `reducer_observability_coverage_correlation`
facts: anchor-or-400, `limit`-required, deterministic `ORDER BY fact.fact_id
ASC` keyset paging on `fact_id`, and the active-generation join filtered to
`is_tombstone = FALSE` and `generation.status = 'active'`, so duplicate facts,
tombstoned (stale) coverage, and inactive generations never leak into a page.
The same page can be filtered by `source_class` and `resource_class` without
making either field a whole-store anchor.

`PackageRegistryHandler` (`package/registry/handler.go`) exposes cheap-summary
aggregates over the graph (:Package) corpus through a separate graph-backed
aggregate read model (`package/registry/aggregates.go`). This is the first
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

`SupplyChainHandler` (`handler.go`) also exposes cheap-summary
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
enums the list endpoint advertises in `openapi/paths/supply/chain/sbom_attestations.go`,
so typos surface as 400 instead of silently returning zero counts.

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
`list_documentation_findings`. Migration 065 adds the order-first partial index
for the ordinary unfiltered page, and migration 066 adds the filter-first
partial index for fully selective six-filter pages. The final schema and migration 003 retain
the distinct ACL-filtered aggregate-visible index. Its predicate exactly
matches these total and grouped aggregate scans, while the ordered keys cover
all five grouping dimensions plus `document_id`.

The `/documentation/facts` free-text surface was evaluated in this issue. Valid
GIN candidates improved read latency, but the fastest option more than doubled
the exact production streaming-write cost. The unchanged 1.6-million-row local
search finished in 1.504 seconds, so no free-text index ships.

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
`repo.uid = image.repository_id` (`impact/trace_deployment_oci.go`), and tag
reads start from `ContainerImageTagObservation.image_ref`, join the repository
with `repo.uid = tag.repository_id`, then match the resolved digest image
(`impact/trace_deployment_oci.go`). The read helper runs one bounded query
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
