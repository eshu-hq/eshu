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
`PackageRegistryHandler` (`package_registry.go:21`) keeps package-registry
reads bounded: package and version identity lookups require a package,
ecosystem, or version anchor, dependency lookup requires `package_id` or
`version_id` plus `limit`, and correlation lookup requires `package_id` or
`repository_id` plus `limit`. Dependency routes return package-native
dependency truth only; correlation routes expose reducer-owned ownership
candidates, provenance-only publication evidence, and admitted manifest-backed
consumption without letting package source hints become ownership truth.
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
`CICDHandler` (`ci_cd.go:16`) reads reducer-owned CI/CD run correlation facts
from Postgres. It requires an explicit scope, repository, commit, provider-run,
artifact-digest, or environment anchor plus `limit`, and it keeps CI success,
environment observations, and shell-only hints separate from deployment truth.
`ServiceCatalogHandler` (`service_catalog.go:16`) reads reducer-owned service
catalog ownership and drift correlation facts from Postgres. It requires an
explicit scope, entity, repository, service, workload, or owner anchor plus
`limit`, and it keeps catalog declarations provenance-only until reducer
evidence corroborates repository, service, workload, ownership, or drift truth.
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
The same handler exposes supply-chain impact findings through a separate
Postgres read model. Impact reads require a CVE, package, repository, subject
digest, or status anchor plus `limit`, and keep CVSS, EPSS, KEV, reachability,
fixed-version state, and missing evidence as separate fields.
Impact responses also attach a `readiness` envelope built by
`BuildSupplyChainImpactReadiness` (`supply_chain_impact_readiness.go:121`) so a
zero-finding result is classified as `not_configured`, `target_incomplete`,
`evidence_incomplete`, `unsupported`, `ready_zero_findings`, or
`ready_with_findings`. The envelope echoes the bounded target scope, lists
per-family fact counts and `latest_observed_at` for `vulnerability.advisory`,
`vulnerability.exploitability`, `package.consumption`, `package.registry`,
`sbom.component`, `sbom.attestation`, and `container_image.identity`, and
returns the stable `missing_evidence` reasons `advisory_sources`,
`owned_packages`, `sbom_or_image_evidence`, `target_collection_incomplete`,
and `unsupported_target`. `PostgresSupplyChainImpactReadinessStore`
(`supply_chain_impact_readiness_postgres.go:18`) runs one bounded CTE per
response with seven anchored counts and a `vulnerability.source_snapshot`
roll-up. The readiness path never invents findings, never duplicates reducer
matching, and adds one Postgres round trip alongside the existing impact
read; observability stays on the existing `query.supply_chain_impact_findings`
span and the `eshu_dp_postgres_query_duration_seconds` histogram.

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
