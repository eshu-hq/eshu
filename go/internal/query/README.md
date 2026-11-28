# internal/query

## Purpose

`internal/query` owns the HTTP read surface, OpenAPI assembly, response envelope
contract, and all read models that back the public Eshu query API. It defines the
`GraphQuery` and `ContentStore` ports through which every handler accesses the
graph and Postgres content store, and it enforces the capability matrix that
determines which queries are permitted under each runtime profile.
For a multi-type NornicDB Function relationship story, the content entity ID
is the canonical graph `uid`. The handler resolves that anchor once per request
and checks legacy `id` only when no canonical anchor exists; an unrelated
legacy-ID collision cannot contribute edges to the selected content entity.
Repository-scoped entity resolution and graph code search start from the indexed
`Repository.id` anchor, then expand only that repository's files and entities;
they do not scan the entity universe before applying repository scope. A
canonical `content-entity:` resolution request is served from the authoritative
content row first. A missing canonical content row is an explicit empty result,
not a reason to fall back to a broad graph name scan.
Global code-name search and typed entity resolution do not use an unanchored
graph scan. They call the narrow `EntityNameSearcher` extension on
`ContentReader`, which applies repository grants, normalized language variants,
entity type, and case-sensitive exact or substring matching before a stable
limit. Untyped global entity resolution, unknown types, graph-only container
types, and global substrings shorter than three Unicode characters fail closed.

`GET /api/v0/graph/entities` reads the eight fixed facet counts in one graph
round trip. Each scalar `CALL` subquery stays anchored on its concrete label and
returns one named count column, including when the label is empty. The outer
projection does not aggregate the columns because NornicDB corrupts that shape.
Go validates every count before restoring the display order. A selected facet
adds one bounded label-anchored list read; an unfiltered request performs no
second graph call. Missing, extra, malformed, or negative count columns return
an error rather than an incomplete inventory.

NornicDB direct-relationship metadata uses the content entity type as its graph
label. When the content row is absent, the compatibility fallback probes the
fixed supported label vocabulary by `uid` and then legacy `id`, optionally
under the selected repository. It never issues an unlabeled property scan.
The static `/api/v0/capabilities` route serves the embedded capability catalog
with the built-in role, grant, data-class, and per-capability authorization
metadata that also backs the MCP capability-catalog tool. Its OpenAPI schema
documents the catalog's `restricted` and `sensitive` data-class sensitivity
values so generated clients can model the role catalog accurately.
The static `/api/v0/surface-inventory` route serves the embedded surface
inventory with collector `collector_contract` provenance: emitted fact kinds,
projection/read consumers, proof gates, fixture references, and the truth profile
that separates deterministic, provider-gated, and optional semantic output.
Code-quality routes also classify graph-derived findings before they reach
HTTP, MCP, or CLI callers; `code_quality.dead_code` returns candidate evidence,
language maturity, exclusions, and truth metadata instead of presenting a raw
Cypher scan as a cleanup list. The dead-code incoming-edge probe is
provenance-weighted (#2719) and now prefers reducer-materialized
`code_reachability_rows` (#2718): a candidate reachable only by a weak
`repo_unique_name` path classifies as `ambiguous` rather than confidently
reachable or `unused`.

## Where this fits in the pipeline

```mermaid
flowchart LR
  Client["HTTP client / MCP"] --> AR["APIRouter.Mount\n(handler.go)"]
  AR --> RH["RepositoryHandler\nEntityHandler\nCodeHandler\n..."]
  RH --> GQ["GraphQuery port\n(Neo4jReader.Run / RunSingle)"]
  RH --> CS["ContentStore port\n(ContentReader)"]
  GQ --> Graph["Graph backend\n(Neo4j / NornicDB)"]
  CS --> PG["Postgres\ncontent_files\ncontent_entities"]
  RH --> RE["ResponseEnvelope\nTruthEnvelope\nErrorEnvelope"]
```

## Internal flow

```mermaid
flowchart TB
  A["HTTP request arrives\nat ServeMux route"] --> B["handler method\n(e.g. RepositoryHandler.getRepositoryContext)"]
  B --> C{"capability supported\nfor profile?"}
  C -- no --> D["WriteContractError\n(handler.go:36)\nErrorCodeUnsupportedCapability"]
  C -- yes --> E["read GraphQuery\nor ContentStore"]
  E -- graph --> F["Neo4jReader.Run\nor RunSingle\n(neo4j.go:34 / 81)"]
  E -- content --> G["ContentReader method\n(content_reader.go)"]
  F --> H["collect result rows"]
  G --> H
  H --> I["build response model\nclassification + truth metadata"]
  I --> J{"Accept: application/eshu.envelope+json?"}
  J -- yes --> K["WriteSuccess\n(handler.go:23)\nResponseEnvelope{Data, Truth, Error}"]
  J -- no --> L["WriteJSON\n(handler.go:14)\nlegacy payload shape"]
```

## Lifecycle / workflow

An HTTP request hits one of the routes registered by `APIRouter.Mount`
(`handler.go:140`). The handler method first checks whether the requested
capability is allowed for the current `QueryProfile` using
`querycontract.CapabilityUnsupported` (`querycontract/http.go:132`), which
the profile does not support the capability, `WriteContractError` returns HTTP
501 with a structured `ErrorEnvelope` carrying `ErrorCodeUnsupportedCapability`,
the capability ID, and the `RequiredProfile`.

For permitted requests, the handler reads data through `GraphQuery` (for graph
traversals) or `ContentStore` (for Postgres content). `Neo4jReader.Run` and
`Neo4jReader.RunSingle` (`neo4j.go:34`, `neo4j.go:81`) open a read-only Neo4j
session, execute a Cypher query, and return `[]map[string]any` rows. Row values
are extracted via `StringVal`, `BoolVal`, `IntVal`, `StringSliceVal`
(`neo4j.go:120`). `ContentReader` methods (`content_reader.go:44`,
`content_reader_entity.go:13`) issue parametrized Postgres queries against
`content_files` and `content_entities`.

Read-model details stay package-local but out of the top-level index:

- [read-models.md](read-models.md) covers entity-map traversal, package
  registry bounds, CI/CD, service catalog, Kubernetes, and observability
  coverage correlations, supply-chain read models, OCI deployment trace
  enrichment, and
  investigation-route read models.
- [dead-code-reachability.md](dead-code-reachability.md) covers dead-code
  language reachability, exactness blockers, candidate paging, hydration,
  observed blockers, and the language-specific suppression contract.
- #5563 (retired evidence record)
  records the authorized owner-ledger page boundary, exact graph hydration,
  upgrade backfill, production variant family, interactive SLO, and
  retained/synthetic evidence for cloud resource browsing. API and MCP startup
  run the backfill before mounting routes. It keyset-pages existing
  `CloudResource.uid` values into `graph_node_owner`, then records a durable
  completion marker so later restarts skip the graph scan.

At the package boundary, all query routes stay anchored, bounded, and explicit
about truth level. Graph reads go through `GraphQuery`, content reads go through
`ContentStore`, and response models keep provenance-only evidence separate from
canonical graph or reducer truth.
Import-dependency investigation uses one connected graph pattern per read.
Module filters anchor the exact module name, resolve bounded repository-file
membership, and reject path collisions before import or call paging. Python
cycles are rebuilt from one ordered edge scan, then directional file and module
filters are applied to the completed cycles so reciprocal edges remain visible.
Internal candidate reads stop at 25,000 rows and return HTTP 422 when the caller
must narrow scope. Package pages are distinct and ordered by repository, module,
and language. The
`query.import_dependency_investigation` span records query type, result count,
truncation, and scan overflow.
Call-graph metrics also use one repository-scoped edge pass. The graph query
anchors both `Function` endpoints by indexed `repo_id` and reads each physical
directed `CALLS` edge in one pass, with a 50,001-edge sentinel. Go then
deduplicates caller/callee pairs by canonical `Function.uid` with a legacy `id`
fallback, computes hub degree or reverse-edge recursion, applies the language
filter, sorts exact ties by canonical identity, builds source and partner entity
handles from that identity, and pages the finished result. Repositories with
more than 50,000 physical `CALLS` edges
fail closed with HTTP 422 and no partial metric rows. This avoids unbounded
materialization and avoids backend-specific multi-clause aggregate and
mutual-edge shapes on this path while preserving self-calls and duplicate-edge
semantics within the exact bound.
Semantic and hybrid search freshness reads require both an identity-scoped
`search_vector_ready` watermark, no active document projection in a non-ready
state, and no pending versioned vector scope for a non-empty ready projection.
A stale watermark from an earlier projection therefore fails closed; keyword
mode does not consult the vector signal.
Repository relationship rows and relationship evidence drilldown normalize
correlation confidence provenance with `confidence_basis`. The value is
`evidence_constant` for a single extractor weight, `evidence_aggregate` for
resolver corroboration, or `assertion_override` for explicit relationship
assertions. Code `CALLS`/`REFERENCES` rows keep using `resolution_method`;
correlation readers compare the numeric `confidence` field only after checking
the basis/method field.
Relationship-story rows expose the same distinction through a uniform
`provenance` block with confidence state, method/source family, reason, truth
state, and derived/heuristic/unsupported flags. The block is built from already
returned row metadata and does not change canonical admission, graph writes, or
the answer-level truth envelope.
SQL-table blast radius uses only writer-backed branches for `READS_FROM`,
`REFERENCES_TABLE`, `WRITES_TO`, `TRIGGERS`, `INDEXES`, `MIGRATES`, and embedded
queries. View reads are followed through at most two `READS_FROM` edges, while
the response keeps `MAPS_TO_TABLE` as an explicit unmaterialized coverage gap.
Pre-change impact reads (`POST /api/v0/impact/pre-change`) are a productized
entrypoint over the existing change-surface investigation contract. They accept
repo-relative changed paths or structured file changes, preserve deleted,
renamed, and copied status, reject absolute and parent-traversal paths, and
return the same bounded code surface, graph impact rows, coverage, truncation,
answer metadata, and AnswerPacket-shaped response instead of creating a parallel
impact model. Optional base/head refs are provenance for caller-derived changed
inputs; refs alone do not trigger server-side diff derivation.
Developer change plan reads (`POST /api/v0/impact/developer-change-plan`) reuse
that normalized evidence path and return a read-only `developer_change_plan.v1`
artifact. The handler maps missing evidence, affected entities, recommended
tests, bounded next calls, action steps, and patch guidance from the pre-change
impact response; it never generates or applies source patches, and it marks the
plan blocked when required evidence is absent or partial.
Repository context relationship queries include reducer-owned
`CORRELATES_DEPLOYABLE_UNIT` graph edges so deployable-unit correlation readback
uses the same confidence, evidence kind, reason, resolved id, and
resolution-source fields as other typed repository relationships.
Admission-decision reads (`GET /api/v0/evidence/admission-decisions`) are the
pre- or side-graph explanation layer for reducer correlation domains. They are
bounded by `domain`, `scope_id`, and `generation_id`, can be narrowed by shared
state or by an anchor such as repository, service, workload, cloud resource,
package, or incident, and expose source handles plus recommended next calls.
They explain why a candidate was admitted, rejected, ambiguous, stale, missing
evidence, permission hidden, unsupported, or unsafe; they do not promote a
candidate to canonical graph truth unless the reducer's canonical write block
shows an admitted write.

Live evidence bundle reads (`GET /api/v0/evidence/bundle`, #4045) compose a
share-safe `evidence_bundle.v1` artifact from the same status providers behind
`GET /api/v0/status/index`, `GET /api/v0/status/pipeline`, and `GET
/api/v0/status/collectors` (`evidence/bundle_live.go`), so `eshu evidence
bundle export --live` and this route describe one running stack through the
same `status.Report` and repository-count query. `EvidenceHandler` only maps
that typed report into an `evidencebundle.LiveSnapshot`; it never re-derives
`internal/evidencebundle`'s redaction or composition rules, calling
`BuildLiveBundle -> Validate -> StampValidation` exactly as the CLI does. The
bundle is stack-wide -- none of the composed data carries a repository or
tenant selector -- so the route carries no `x-scoped-token-support` marker and
is absent from `scopedHTTPRouteSupportsTenantFilter`, the same posture as its
full-report source routes; a scoped-bearer-token caller is always rejected
by `AuthMiddleware` before the handler runs. A browser-session caller's
admission is policy-dependent, not a flat reject: off the allowlist, as here,
`browserSessionRouteDenialReason` admits only a tenant-bound all-scopes
session under `BrowserSessionRoutePolicy.AllowTenantBoundAllScopes`, which
`cmd/api` sets for the default, `local_no_policy`, and `hosted_single_tenant`
modes, and rejects a restricted-scope session. On the allowlist it splits by
`scopedRouteClass` (#6450): an all-scope session still needs that policy
unless the route needs no caller grant.

Version resolution selects the strongest eligible concrete claim in
deployment-truth
order: runtime-observed evidence, CI-declared provenance, then config evidence.
The unshipped declared-ref tier contributes no claim. A CI-declared digest that
contradicts the finding's subject digest remains corroboration and cannot win.
Same-axis claims are labeled `agrees` or `disagrees`; digest, image-reference,
and package-version claims on different axes are `not_comparable`.
The cloud-runtime owner-ledger read bounds rows per requested digest. A
`CROSS JOIN LATERAL` driven by the distinct digest set gives every digest its
own bounded, ordered `(digest, ARN, uid)` index scan, capped at the 200-row page
budget divided across the digests on the page with a floor of 10
(`supplyChainCloudRuntimeProbePerDigestLimit`). Active-generation and
caller-grant checks run inside that lateral, before its limit, so the bound
counts eligible rows: a digest whose first rows are stale or outside the
caller's grants still yields the later row that is neither. A digest running on
more resources than its share reports only the first slice of them, but no
digest is left with no runtime evidence at all (#5789). The matching migration
086 partial index excludes blank and whitespace-only digest or ARN values, so
empty cloud inventory rows do not amplify storage or writes.

On Postgres 18 with 100,000 synthetic source rows, 50,000 canonical keys, and
500 operator suppressions, 20 warm executions on the same machine measured the
baseline median/p95 at direct list 177.170/184.338 ms, materialized list
60.163/61.190 ms, aggregate count plus facets 465.099/468.035 ms, explain
63.906/65.402 ms, and winner rebuild 912.147/943.861 ms.

The final runtime implementation commit
`63d06f6cc0d06ca9bc97a7ffa24b9eb4a8eb5d88` was measured with:

```bash
cd go
GOCACHE=$PWD/../.gocache \
  ESHU_POSTGRES_TEST_DSN='postgresql://eshu:change-me@127.0.0.1:25432/eshu?sslmode=disable' \
  go test ./internal/query/supply/chain/impact \
  -run '^TestSupplyChainSuppressionPathsPerformanceLive$' -count=1 -v
```

The final branch median/p95 measurements were direct list 136.923/138.221 ms,
materialized list 14.636/15.762 ms, aggregate count plus facets
342.687/349.311 ms, public finding-ID explain 2.909/3.074 ms, and winner rebuild
873.095/918.479 ms. The post-documentation verification rerun on the same
runtime code measured 141.541/151.742, 15.822/17.058, 352.147/382.474,
2.895/3.193, and 919.997/1018.404 ms, respectively. All five paths stayed
within the 10% stop threshold on both runs. Direct, materialized, aggregate,
and explain improved on both samples. Winner rebuild improved on the first and
was 0.9% slower at median and 7.9% slower at p95 on the verification sample,
which remains within the declared no-regression band.

The test applies migration 084 concurrently after seeding the populated fact
table, proving the upgrade DDL before timing the production query. A
pre-implementation query shim compared the broad explain shape with the indexed
public-ID shape on the same data: the exact row differential was zero, while
median/p95 improved from 68.502/77.519 ms to 15.950/18.338 ms. The permanent
test asserts direct/materialized output equality and exact total, priority, and
severity facets across all 50,000 canonical keys. Focused dispatch tests prove
that public finding IDs use the indexed query, raw fact IDs and canonical keys
fall back to the compatibility query only after an indexed miss, and ambiguity
remains fail-closed.

A rejected eager expiry overlay measured 40.950 ms versus 27.959 ms. A rejected
composite authority sort measured 187.286/193.673 ms. The explain compatibility
anchor initially regressed to 116.815/122.970 ms; a proven `NOT MATERIALIZED`
shape preserved the exact explanation and recovered 64.621/69.648 ms before it
was applied. The 100,000-winner expiry migration measured 11.254 ms on first
apply and 6.337 ms on repeat, changed only the two intended operator rows, and
performed 2,331 shared-buffer hits with zero reads. The final warm
`EXPLAIN (ANALYZE, BUFFERS)` public-ID explain plan executed in 0.549 ms with
78 shared-buffer hits, zero reads, and zero temporary blocks.
The advisory-filter compatibility path preserved its exact legacy row set at
16.114/17.041 ms versus 16.849/18.142 ms before the guard; the new
advisory-only path returned exactly 250 intended rows at 18.452/23.607 ms.

The same-machine built-binary golden gate ran on fresh volumes at exact runtime
head `63d06f6cc0d06ca9bc97a7ffa24b9eb4a8eb5d88`, based on
`a2a5340a9e4bd975a8d4ccd97be547bb5b9e84b3`, with:

```bash
docker compose -f docker-compose.yaml down -v
GOCACHE=$PWD/.gocache ESHU_POSTGRES_PORT=25432 \
  NEO4J_HTTP_PORT=27474 NEO4J_BOLT_PORT=27687 \
  bash scripts/verify-golden-corpus-gate.sh
```

It completed in 109 seconds with 511 passes, zero required failures, and one
advisory timing warning. Bootstrap/collect/first-drain/maintenance/graph-query
phases were `5/21/66/17/24` seconds. The graph/query advisory is expected
because the accuracy proof deliberately waits for a future suppression to
expire; it is not production pipeline work. The exact advisory was
`phase_graph_query: observed=24.0s, baseline=3.0s, ceiling=8.0s`.
First-drain and every maintenance/suppression drain ended with zero residual
and dead-letter work. Scope setup took 0.007944 seconds, the ignored mutation
took 0.004450 seconds, the ignored drain took 2.095769 seconds, and seven
identical retries had a 0.003336-second handler median without adding a
generation or work item. Active, hidden, audit, and expired finding reads had
medians of 0.021644, 0.004942, 0.010544, and 0.014689 seconds.

Documentation findings, documentation evidence packets, and semantic-evidence
rows surface an optional bounded `source_acl_state` (`allowed`, `denied`,
`partial`, `missing`, `stale`) the collector observed on the fact's
`acl_summary` (#2164, #1901 vertical). It is a DISTINCT access-posture axis kept
separate from freshness and the binary permission decision: a row can be fresh
yet denied, or stale yet allowed, and `partial` or `stale` ACL — which the binary
permission flag cannot express — has its own representation. The reader fails
closed: only a bounded value from `docs.ValidSourceACLState` is read verbatim (a
`boundedSourceACLState` helper drops empty, absent, or non-bounded values), so
absence means "no ACL claim."

On top of that surfacing, `source_acl_disclosure.go` enforces the USER-APPROVED
disclosure policy (#2164). `applySourceACLDisclosure` maps the bounded posture
and the per-caller read decision onto a bounded `access_disposition`
(`visible`/`access_denied`/`partial`/`stale`/`missing`) and, for any posture that
is not cleanly readable, strips every field outside a bounded identity/state
allowlist so no protected content/excerpt is returned:

- `denied` (or a binary-denied caller) → `access_denied` with
  `permission_denied: true`, `content_withheld: true`, content stripped. The
  findings list discloses the row instead of silently dropping it; the single
  evidence-packet read returns `403 permission_denied`.
- `partial` → `partial` marker with `content_withheld: true`.
- `stale` → surfaced as stale, content intact (permitted-but-stale).
- `missing` → disclosed as missing.
- `allowed`/no-claim → `visible`.

Withholding is fail-closed (allowlist, not denylist) and the #2138 truth labels
(`freshness_state`, `truth_level`, `admission_state`, `corroboration_state`,
`missing_evidence`, `unsupported_reason`) are preserved on a withheld row and
never collapsed into the access marker. Per-row dispositions are recorded as
bounded, content-free span attributes (`eshu.query.source_acl.*_count`); no
source id, path, title, url, or user identity appears in any label.

Component extension reads expose runtime component registry state through
`GET /api/v0/component-extensions` and
`GET /api/v0/component-extensions/{component_id}/diagnostics`. These routes read
`component.Registry.Readback` only when the API or MCP runtime was configured
with `ESHU_COMPONENT_HOME`; otherwise they return a canonical unavailable
envelope. Responses include component ID, version, publisher, manifest digest,
lifecycle states, activation `config_handle` values, and policy diagnostics.
Inventory responses are bounded by `limit` and carry `count`, `total_count`,
and `truncated`; neither inventory nor diagnostics exposes local manifest paths,
activation config paths, or community-index membership as trust.

CODEOWNERS ownership reads use one indexed graph query for an initial page and
three mutually exclusive indexed queries for a cursor page. The handler merges
the cursor branches back into the documented `(order_index, pattern,
owner_ref)` order and retains only `limit+1` rows. This avoids a pinned
NornicDB mixed-`OR` failure that silently produced an empty page while keeping
the maximum in-process merge bounded to 603 rows. The live exactness and timing
proof is recorded in #5419 (retired evidence record).

## Exported surface

The package exports four groups of contracts:

- Ports and adapters: root aliases for querycontract's `GraphQuery` and
  `ContentStore`, plus `Neo4jReader`, `ContentReader`, metrics sources, and
  route-specific stores such as
  supply-chain readiness, advisory evidence, IaC reachability, freshness, and
  metrics time-series ports.
- Handler structs: `APIRouter` plus route owners including repository, entity,
  code, content, component-extension, infrastructure, IaC, impact, evidence,
  documentation, semantic-evidence, supply-chain, incident, work-item,
  codeowners-ownership, freshness, status, metrics, compare, and admin
  handlers.
- Response contracts: root aliases for querycontract's `ResponseEnvelope`,
  `TruthEnvelope`, `ErrorEnvelope`, `AnswerPacket`, query playbooks,
  investigation workflows, visualization
  packets, and the typed constants that describe truth level, freshness,
  profile, backend, and errors.
- Helpers: compatibility wrappers over querycontract's response writers,
  JSON/path/query parsers and truth-envelope builder, plus bearer-token
  middleware, OpenAPI assembly, and graph row extraction helpers.

See `doc.go` for the godoc contract, `read-models.md` for route-specific
read-model bounds, and public reference docs for long-lived API/MCP wire
contracts. Keep detailed route evidence near the owning implementation or
scoped `AGENTS.md` entry instead of expanding this index.

## Dependencies

- `internal/query/querycontract` — dependency-neutral profiles, envelopes,
  capability registry, HTTP helpers, ports, and port model types
- `internal/buildinfo` — `AppVersion()` embedded in the OpenAPI spec
- `internal/contentrefs` — content reference utilities used in content query paths
- `internal/correlation/cloudinventory` — `ResolveProviderIdentity`, used by
  `awsCloudRuntimeDriftRowToNeutral` (`cloud_runtime_drift_aggregate.go`) to
  compute the same canonical `cloud_resource_uid` keyspace for an AWS-origin
  finding merged onto the provider-neutral runtime drift surface that GCP/Azure
  findings already carry (#5759 follow-up)
- `internal/iacreachability` — IaC reachability row types consumed by
  `PostgresIaCReachabilityStore`
- `internal/parser` — entity and language classification constants used for
  dead-code root detection
- `internal/recovery` — `RecoveryService` port satisfied by `recovery.Handler`;
  wired into `AdminHandler.Recovery`
- `internal/status` — `status.Reader` consumed by `StatusHandler.StatusReader`
- `internal/storage/postgres` — status, recovery, IaC reachability, AWS
  runtime drift finding, and multi-cloud/AWS aggregate runtime drift finding
  (`MultiCloudRuntimeDriftFindingStore.ListActiveFindingsAcrossProviders`,
  #5759 follow-up) adapters; query handlers never import concrete Postgres
  drivers directly — they go through query package adapters and ports
- `internal/telemetry` — `EventAttr`, `DefaultServiceNamespace`, span constants
  `SpanQueryRelationshipEvidence`, `SpanQueryDeadIaC`,
  `SpanQueryIaCUnmanagedResources`, `SpanQueryIaCTerraformImportPlan`, `SpanQueryInfraResourceSearch`, `SpanQueryCodeTopicInvestigation`,
  `SpanQueryHardcodedSecretInvestigation`, `SpanQueryDeadCodeInvestigation`,
  `SpanQueryChangeSurfaceInvestigation`, `SpanQueryWorkItemEvidence`

Handlers depend on the `GraphQuery` and `ContentStore` ports, not on
`neo4jdriver.DriverWithContext` or `*sql.DB` directly. `Neo4jReader` and
`ContentReader` are the only concrete types that touch drivers, and they are
wired in `cmd/eshu-local-api/wiring.go`, not here.

Semantic extraction status is a runtime projection.
`GET /api/v0/status/semantic-extraction` reports no-provider mode as
`unavailable` with code hints and documentation observations disabled.
When semantic provider profiles are configured, the route includes redacted
`provider_profiles[]` rows with profile id, provider kind, model metadata,
embedding dimensions, credential source kind, source classes, source-policy
state, and profile health/configuration state. It does not expose credential
handles or raw keys. Governed `search_documents` profiles may feed the semantic
search vector read/build lane, while canonical graph truth and documentation
fact routes remain unaffected. Its fenced reader selects only semantic SQL; semantic and freshness failures still fail the route.

Answer narration status is a runtime status projection, not a narration
generator. `GET /api/v0/status/answer-narration` reports the optional governed
narration posture as disabled by default, with deterministic answer packets
available as the canonical fallback. The route exposes low-cardinality state,
reason, retention posture, policy hash, and validator reason-code metadata only;
it must not emit prompts, provider responses, credential handles, source
identifiers, local paths, or canonical truth changes. The route uses capability
`answer_narration.status` and shares its handler with MCP
`get_answer_narration_status`.

Hosted governance status is also a runtime status projection. `GET
/api/v0/status/governance` reports only redacted governance mode, policy state,
source kind, optional policy revision hash, readiness booleans, aggregate
counts, and low-cardinality reason codes. Its `audit` section reports event,
denied, unavailable, event-type, actor-class, scope-class, reason, and ACL-state
counts only. It derives explicit runtime readback from `ESHU_GOVERNANCE_*`
settings, the private `governance_audit_events` aggregate summary when wired,
and semantic aggregate status; it must not emit raw policy JSON, tenant or
workspace identifiers, repository or source identifiers, detailed audit hashes,
correlation ids, credential handles, provider endpoints, prompts, provider
responses, local paths, or token values. The route uses capability
`hosted_governance.status` and shares its handler with MCP
`get_hosted_governance_status`.

Ingester status is a runtime status projection from `internal/status`.
`GET /api/v0/status/ingesters` and
`GET /api/v0/status/ingesters/{ingester}` remain shared-token status routes for
full operational readback, but scoped-token detail responses expose coordinator
counts instead of collector instance rows. Scoped responses keep runtime health,
queue, stage-summary, scope-activity, and domain-backlog counters visible while
leaving collector instance identifiers, display names, tenant and workspace
values, queue conflict keys, repository/source identifiers, provider payloads,
local paths, and credentials out of the payload.

Collector status is the same runtime-status family. `GET
/api/v0/status/collectors` keeps the shared-token per-instance readback for
operators, but scoped-token responses collapse collector runtimes into aggregate
rows keyed by safe operational categories. Scoped responses keep collector kind,
runtime mode, status category, health, counts, evidence-source summaries, and
aggregate timestamps while leaving collector instance identifiers, display
names, source-system detail, queue conflict keys, tenant/workspace values,
provider payloads, local paths, and credentials out of the payload. The legacy
`/api/v0/collectors` alias remains unavailable to scoped tokens.

The live operations board at `GET /api/v0/status/operations` is exact and
complete for all-scopes operators: it combines process-wide health, collector,
stage, domain-backlog, and queue aggregates with bounded live activity. A
scoped caller receives only live-activity rows restricted to its granted
repositories or ingestion scopes, with repository and worker identities
redacted. The process-wide aggregate sections are omitted because they cannot
be attributed to those grants; `completeness_state=scoped_live_activity_only`,
`withheld_sections`, and a derived truth level make that boundary explicit.

Hosted readiness follows the same aggregate rule for scoped tokens. `GET
/api/v0/status/hosted-readiness` keeps hosted readiness checks, queue counters,
collector-generation replay blockers, repository counts, diagnostic route
names, and coordinator aggregate counters visible, but it replaces coordinator
instance rows with aggregate counts so
collector instance identifiers, display names, queue conflict keys,
tenant/workspace values, provider payloads, local paths, and credentials stay
out of the payload.

`GET /api/v0/status/collector-readiness` (alias `/api/v0/collector-readiness`)
projects `collector.PromotionProofs` over the full collector fleet into a
redacted, console-consumable readiness read model: per-family promotion state,
runtime category, health, claim state, reducer readback status, evidence counts,
last proof time, blockers, and a recommended next action. The MCP tool
`get_collector_readiness` maps to the same route so API and MCP return identical
readiness truth. Scoped tokens drop the per-instance identifier so a scoped
caller sees family-level readiness only; full per-grant `permission_hidden`
filtering is tracked as a follow-up.

## Telemetry

- Spans: `telemetry.SpanQueryRelationshipEvidence` (`query.relationship_evidence`)
  on evidence drilldown and `telemetry.SpanQueryEvidenceCitationPacket`
  (`query.evidence_citation_packet`) on citation packet hydration. Hydrated
  citations carry the unified evidence contract (issue #3489): `confidence`, a
  byte-level citation (`byte_offset`/`byte_length`, `content_hash`,
  `commit_sha`), and a typed `provenance` object. `citationToCanonical`
  round-trips one citation into `truth.Evidence`.
  `telemetry.SpanQueryDocumentationFindings`
  (`query.documentation_findings`),
  `telemetry.SpanQueryDocumentationFacts`
  (`query.documentation_facts`),
  `telemetry.SpanQueryDocumentationEvidencePacket`
  (`query.documentation_evidence_packet`), and
  `telemetry.SpanQueryDocumentationPacketFreshness`
  (`query.documentation_packet_freshness`) on documentation truth evidence
  routes (`documentation.go`); `telemetry.SpanQueryDocumentationAggregate`
  (`query.documentation_aggregate`) on documentation finding count and
  inventory routes (`documentation_finding_aggregates_handler.go`); the
  target-fact preview adds a `postgres.query` span with
  `db.operation=list_documentation_target_facts`;
  `telemetry.SpanQuerySemanticEvidence` (`query.semantic_evidence`) on opt-in
  semantic observation and code-hint fact reads;
  `telemetry.SpanQueryCodeTopicInvestigation`
  (`query.code_topic_investigation`) on broad code-topic investigation
  (`code_topic.go`); `telemetry.SpanQueryHardcodedSecretInvestigation`
  (`query.hardcoded_secret_investigation`) on redacted hardcoded-secret
  investigation (`code_security_secrets.go`);
  `telemetry.SpanQueryDeadCodeInvestigation`
  (`query.dead_code_investigation`) on dead-code investigation
  (`code_dead_code_investigation.go`); `telemetry.SpanQueryChangeSurfaceInvestigation`
  (`query.change_surface_investigation`) on change-surface investigation;
  `telemetry.SpanQueryResourceInvestigation`
  (`query.resource_investigation`) on resource investigation;
  `telemetry.SpanQueryDeadIaC` (`query.dead_iac`)
  on IaC dead-code queries (`iac/handler.go`); `telemetry.SpanQueryIaCUnmanagedResources`
  (`query.iac_unmanaged_resources`) on AWS management finding list queries,
  `telemetry.SpanQueryIaCManagementStatus` (`query.iac_management_status`) on
  exact status reads, and `telemetry.SpanQueryIaCManagementExplanation`
  (`query.iac_management_explanation`) on grouped evidence explanations;

  `telemetry.SpanQueryIaCTerraformImportPlan`
  (`query.iac_terraform_import_plan`) on read-only Terraform import-plan
  candidate generation; `telemetry.SpanQueryAWSRuntimeDriftFindings`
  (`query.aws_runtime_drift_findings`) on active AWS runtime drift finding
  reads;
  `telemetry.SpanQuerySupplyChainImpactExplanation`
  (`query.supply_chain_impact_explanation`) on one-finding vulnerability
  explanations;
  `telemetry.SpanQueryInfraResourceSearch`
  (`query.infra_resource_search`) on infrastructure search (`infra.go`);
  `telemetry.SpanQueryWorkItemEvidence`
  (`query.work_item_evidence`) on source-only work-item evidence reads
  (`internal/query/workitem/handler.go`).
  Per-query spans `neo4j.query` and `postgres.query` on every graph and content
  read.
- Metrics: `eshu_dp_neo4j_query_duration_seconds` and
  `eshu_dp_postgres_query_duration_seconds` (instruments live in
  `internal/telemetry/instruments.go`).
- Graph-read policy: every `Neo4jReader` call, including startup owner-ledger
  backfill pages, shares one 10-second client/backend budget and at most one
  fresh-session retry for a typed retryable connectivity failure. The Neo4j
  duration metric uses `operation="read"` with a closed outcome vocabulary;
  caller deadlines remain distinct from graph-policy deadlines. See
  [graph-read-deadlines.md](graph-read-deadlines.md) for the proof record.
- Log events: `repository_query.stage_started`, `repository_query.stage_completed`
  (via `repositoryQueryStageTimer`); `service_query.stage_started`,
  `service_query.stage_completed` (via `serviceQueryStageTimer`). Both emit
  `operation`, `stage`, `repo_id`, and `duration_seconds`.

Answer-facing story and investigation responses attach additive
`answer_metadata` companions with normalized evidence handles,
missing-evidence rows, limitations, truncation, coverage, partial reasons, and
recommended next calls. The companion is derived only from the already-built
response payload or incident response struct; it performs no graph,
content-store, provider, reducer, collector, or queue reads and does not add a
new span. Keep the canonical route fields authoritative and use
`answer_metadata` only as the prompt-facing adapter for AnswerPacket composition
or MCP summary text.

Durable no-regression and observability notes for issue-specific query shapes,
including CloudResource candidate reads and NornicDB predicate compatibility,
live in retired evidence record.

## Operational notes

- High latency on `GET /api/v0/repositories/{repo_id}/context` or story routes:
  check the `repository_query.stage_completed` log events for the slow stage
  (`graph_lookup`, `content_hydration`, etc.) before assuming the graph backend
  is the problem.
- `eshu_dp_neo4j_query_duration_seconds` rising across many routes: check graph
  backend health and query plan; do not raise handler timeouts before confirming
  the Cypher query itself is the bottleneck.
- 501 responses with `error.code=unsupported_capability`: the requested
  operation requires a higher `QueryProfile`. Code-only graph capabilities and
  platform-impact queries start at `local_authoritative`; `local_full_stack`
  uses the same handlers with the Compose runtime shape. Check
  `truth.profiles.required` in the response envelope for the minimum profile,
  then verify the ESHU_QUERY_PROFILE env var in the running API.
- `/api/v0/code/complexity` treats `entity_id` as the exact selector. A
  duplicate `function_name` returns `409` with `error.code=ambiguous` and
  bounded `error.details.candidates`; clients should retry with one returned
  `entity_id` rather than accepting the first name match.
- `/api/v0/code/relationships` returns `target_resolution.status=ambiguous`
  with bounded entity candidates when a repo-scoped name matches multiple
  non-test entities. It does not query the graph or fall back to a first match
  until the caller supplies an exact `entity_id`.
- `OpenAPISpec()` panics at startup if a handler calls `BuildTruthEnvelope` with
  a capability string missing from `querycontract/capability.go`'s registry.
  Register missing capability IDs from `go/internal/query/contract/` first.
- `code_quality.dead_code` is a derived query unless the language maturity row
  says otherwise. Handler changes must preserve `classification`,
  `dead_code_language_maturity`, and `analysis` fields so MCP and CLI callers
  can distinguish actionable unused symbols from excluded or ambiguous ones.
  Go root-kind evidence covers function roots and type roots, including
  `go.dependency_injection_callback`, `go.direct_method_call`,
  `go.fmt_stringer_method`, `go.function_literal_reachable_call`,
  `go.function_value_reference`, `go.generic_constraint_method`,
  `go.imported_direct_method_call`, `go.imported_fmt_stringer_method`,
  `go.interface_implementation_type`, `go.interface_method_implementation`,
  `go.interface_type_reference`, `go.method_value_reference`, and
  `go.type_reference`. JavaScript-family
  analysis must list Node package, CommonJS default export, CommonJS mixin,
  Next.js, Node migration, Hapi-style, TypeScript public API, TypeScript
  module-contract, and TypeScript interface implementation roots, plus Java
  main, constructor, override, Ant Task setter, Gradle plugin `apply`, task
  action/property, and public Gradle DSL roots when query policy suppresses
  those candidates, plus Swift parser-backed roots when query policy suppresses
  those candidates; the analysis notes name the same Java and C root families.
  Rust parser-backed root,
  syntax-evidence, and observed-blocker rows must stay aligned with the
  `deadCodeLanguageMaturity` table because Rust derived classification depends
  on that maturity row, the root suppression policy, and ambiguous
  classification for exactness-blocked candidates.
  The handler scans raw content-model or graph candidates in bounded
  label-scoped pages before policy exclusions, pushes any requested language
  filter into the candidate query, then checks completed reducer code-call
  intent rows for incoming edges on the remaining candidates and uses a
  shared 2,500-row scan window for small result limits. Label pages are
  round-robined inside that ceiling so a saturated early label cannot starve
  later labels or multiply downstream work. It reports the shared maximum as
  `candidate_scan_limit`, the maximum share one label may consume as
  `candidate_scan_limit_per_label`, and
  `candidate_scan_pages` plus `candidate_scan_rows`.
  `display_truncated` and `candidate_scan_truncated` must stay separate so
  performance bounds do not blur result-list pagination with raw scan coverage.
  Unsupported language metadata and repository-root
  `test/`, `tests/`, and `__tests__/` paths stay out of default cleanup results.
- Ready-state hardcoded-secret reads scan `content_file_secret_lines`
  (migration 138, #7125) by primary key to `LIMIT`
  (`content_reader_security_secrets.go`), applying scope, kind, and suppression
  before `LIMIT` and `OFFSET`. Until ready, the legacy `content_files` scan
  preserves results. The Go pattern and `hardcodedSecretSQLSuppressionPredicate()`
  are bound to migration 138 by `hardcoded_secret_migration_binding_test.go`;
  changes need a new re-derivation migration. Keep suppression in SQL and
  `content_files` scans confined to the unready fallback.
- Content reads return `source_backend=unavailable` when Postgres does not have
  a cached row for the requested file. This is not a Postgres error; the ingester
  has not yet written content for that scope.
- `AuthMiddleware` (`auth.go`) skips auth only when the resolved token is empty
  (dev mode) or the method/path is accepted by `publicHTTPRoute`. Adding new
  public routes requires updating that helper. OIDC login and callback are exact
  public GET routes. Dynamic SAML metadata/login/ACS paths are checked by
  `auth_public_routes.go` so provider identifiers can vary without opening a
  wider prefix. All data routes still require bearer or browser-session auth.
- `OAuthProtectedResourceHandler` and `PostureOAuthChallengePolicy`
  (`auth_oauth_discovery.go`, issue #5163, F-2) implement the RFC 9728 discovery
  route and the gated `WWW-Authenticate` challenge. Enablement is derived per
  request from `DeriveAuthPosture` plus the active bearer issuers, never a
  static flag. A `401` augments its challenge only for a credential-less or
  unrecognized-credential denial: `unauthorizedResponse` reads an
  `OAuthChallengePolicy` from the request context that
  `authMiddlewareWithRoutePolicy` attaches at exactly those sites, and the
  resolver-error site keys on `ErrBearerCredentialUnrecognized` (wrapped by the
  bearer resolver only for pre-match outcomes) so a recognized-but-rejected or
  infra-error `401` stays byte-identically bare — the `#59467` fail-safe.
- Browser session routes (`browser_session_handler.go`) exchange an explicit
  scoped credential for host-scoped HttpOnly session and readable CSRF cookies.
  Middleware hashes cookie and CSRF secrets before resolver calls, then attaches
  the same tenant/workspace/scoped grant context handlers already understand.
  Unsafe cookie-authenticated requests require `X-Eshu-CSRF`, and shared API
  keys stay on the bearer path because they do not carry tenant/workspace
  bounds for session creation.
- `writeBrowserSessionCookies` (`browser_session_handler.go`, a forwarder to
  `session.WriteBrowserSessionCookies`) decides the Secure cookie attribute
  per request via `browserSessionCookieSecure` (`auth/session/cookies.go`), gated by each handler's
  `CookieSecure` field (`CookieSecureMode`, env `ESHU_AUTH_COOKIE_SECURE`,
  default `auto`). `cmd/api` resolves the env value once at startup with
  `ValidateCookieSecureMode`, which fails startup closed on an unrecognized
  value (matching `ParseQueryProfile`/`ParseGraphBackend`); the request-time
  `ParseCookieSecureMode` only normalizes the Go zero value (an unset struct
  field) to `auto` and never sees raw operator input. `auto` keeps Secure set
  for every request except a plain-HTTP loopback origin (`localhost`,
  `127.0.0.0/8`, `::1`), which relaxes it so local development without TLS
  keeps a persistent session; any other plain-HTTP origin still gets
  `Secure=true`, so the browser drops the cookie rather than this server ever
  issuing a non-Secure cookie outside loopback. `always` restores the
  pre-#4964 always-Secure behavior. `BrowserSessionHandler`,
  `LocalIdentityHandler`, and `SAMLHandler` all thread the same
  `CookieSecure` value through to this decision (#4964).
- `BrowserSessionCookieName`/`BrowserSessionCSRFCookieName` (`auth.go`) use
  the `__Host-` prefix (RFC 6265bis), which browsers reject outright unless
  Secure is set — so a relaxed, non-Secure cookie cannot use that name.
  `browserSessionCookieNames` (`auth/session/cookies.go`) selects the
  `__Host-` names when Secure and the bare `BrowserSessionCookieNameInsecure`/
  `BrowserSessionCSRFCookieNameInsecure` names when relaxed; every read path
  (`tryBrowserSessionAuth` in `auth.go`, `browserSessionHashFromCookie` in
  `browser_session_handler.go`) goes through `browserSessionCookieValue`,
  which checks both names so a session authenticates regardless of which
  variant issued it. Logout clears both variants unconditionally since the
  handler has no durable record of which one a given browser holds.
- SAML routes (`saml_handler.go`) are public only for metadata, login, and ACS.
  Login stores a RelayState hash with the AuthnRequest ID; ACS requires that
  RelayState, validates the SAML response through `crewjam/saml` (upstream v0.5.2, the GO-2026-6909 fix; see `docs/public/operate/saml-library-advisory.md`; `CrewjamSAMLVerifier.VerifySAMLResponse` recovers a library panic and counts it), reserves a
  replay hash before session creation, and then creates the same browser-session
  cookies as the explicit credential exchange. SAML claims identify an external
  subject; they do not grant permissions until the store resolves them to an
  Eshu `AuthContext`.
- `GitHubLoginHandler.handleCallback`, `OIDCLoginHandler.handleCallback`, and
  `SAMLHandler.handleACS` each record an `identity_authentication`
  governance-audit event for every callback outcome through their `Audit`
  field, mirroring `LocalIdentityHandler.auditLocalIdentity` (issue #5601):
  `sso_login_authenticated` on success, and a denial classification (e.g.
  `org_not_allowed`, `no_grants`) on failure. `sso_login_audit.go` carries the
  classification from `githublogin`/`oidclogin`'s `CompleteXLogin` up through
  `SSOLoginDeniedError`, since that is where the org/grant checks that produce
  it actually run. Before this, SSO logins left no durable trace once their
  `browser_sessions` row expired — the local-login path already audited every
  outcome, but SSO audited none.
- `RequestMetricsMiddleware` (`metrics_alias.go`, forwarding to `metrics/request.go`) wraps the application mux and
  records `eshu_dp_api_request_duration_seconds` and
  `eshu_dp_api_request_errors_total` per endpoint, labeled by the matched route
  pattern and `status_class`. It resolves the route via `mux.Handler` without
  mutating the request, so cardinality stays bounded by registered routes. The
  API and MCP server wire it around their router mux; the admin surface is
  intentionally not counted.

## Extension points

- `GraphQuery` — implement this interface to add a new graph adapter; no handler
  code branches on the backend brand.
- `ContentStore` — implement this interface to swap the Postgres content reader
  for a different backing store.
- `IaCReachabilityStore` — implement this interface for custom IaC reachability
  sources; `PostgresIaCReachabilityStore` is the only shipped implementation.
- `AdminStore` — implement this interface to redirect admin queue operations to
  a different storage layer.
- OpenAPI fragments — add an exported string constant under
  `openapi/paths/<family>/` and reference it in `openapi.Spec()`; that order is
  the served JSON path order. Shared fragments go in `openapi/schema/`.

Do not add `if graphBackend == "nornicdb"` branches in handler code. Backend
dialect differences belong in `internal/storage/cypher` adapters behind the
`GraphQuery` port.

## Safe replay (admin recovery)

`POST /api/v0/admin/replay` (`admin_replay.go`) is a guarded recovery action.
It requires an explicit `reason` and `idempotency_key`, gates on an admin
(all-scopes) token, refuses unsafe failure classes (`input_invalid`,
`unsafe_payload`, and the manual-review dead-letter triage classes
`projection_bug` and `resource_exhausted` sourced from
`projector.ManualReviewTriageClasses`) unless forced, records an
`admin_recovery_action` governance audit event for every accepted or refused
attempt, and dedupes concurrent or duplicate delivery through the durable
`admin_replay_requests` ledger. The unsafe set is the single source of truth
for both the targeted-class refusal and the broad-selector SQL exclusion, so a
poison `projection_bug` never drains via a scope-wide replay without force.

- Conflict domain: `admin_replay_requests.idempotency_key` (primary key) is the
  serialization point; exactly one concurrent request wins the
  `INSERT ... ON CONFLICT DO NOTHING` claim and runs the replay, while losers
  return the recorded outcome (`duplicate=true`) or a `409`. The underlying
  `fact_work_items` replay UPDATE is row-level idempotent
  (`WHERE status IN ('dead_letter','failed')`), so it never overwrites in-flight
  or pending work.

## Gotchas / invariants

- `BuildTruthEnvelope` panics if `capability` is missing from the registry
  (`querycontract/capability.go`). All capability strings used in handlers must be
  registered from `go/internal/query/contract/` before the handler runs.
- `querycontract.CapabilityUnsupported` returns true when `maxTruthLevel` returns
  `nil` for the current profile; a nil max-truth means the capability is
  explicitly unsupported at that profile level. Every handler that gates on a
  capability calls it directly (`infra.go:195`, `cloud_resources.go:77`).
- `Neo4jReader` opens a new session per query by calling `NewSession` on the
  driver (`neo4j.go:50`); the session is closed in a `defer`. Do not hold
  sessions across multiple queries in the same handler.
- `/api/v0/code/cypher` and MCP `execute_cypher_query` are an escape hatch, not
  a general write surface. The handler gates `graph_query.read_only_cypher`
  before it touches `GraphQuery`, rejects mutation/admin tokens before execution,
  appends a bounded probe `LIMIT`, keeps the 30 second graph context deadline,
  and returns structured envelope errors when callers request envelopes.
- `/api/v0/code/visualize` and MCP `visualize_graph_query` share that read-only
  Cypher safety path: the handler gates `visualization.graph_query`, rejects
  mutations, bounds the query with `boundedReadOnlyCypher`, executes it under the
  same 30 second deadline against `GraphQuery`, then projects the returned graph
  nodes, relationships, and paths into a bounded `VisualizationPacket`
  (`visualization.BuildGraphQueryVisualizationPacket`) instead of returning
  a hardcoded browser URL. Scalar-only results yield an explicit
  unsupported packet.
- `ContentReader` traces each Postgres call with an OTEL span labeled
  `db.sql.table`; queries that scan multiple tables need per-call spans to avoid
  misleading attribution (`content_reader.go:45`).
- `ContentReader.SearchEntityNames` is the only global entity-name read. Its
  explicit scope enum prevents an empty repository grant from becoming an
  all-repository query, and `db.operation=search_entity_names` identifies the
  bounded Postgres call in traces. `/api/v0/code/search` asks this store for
  `limit+1`, returns at most the public 200-row ceiling, and reports exact
  `count` / `limit` / `truncated` page metadata. Repository display names are
  hydrated once for the distinct repository IDs in the bounded page; a
  per-result `LATERAL` scan over `ingestion_scopes` is forbidden.
- `/api/v0/code/language-query` treats `entity_type=variable` as pure
  content-backed (`contentBackedEntityTypes`), not graph-first. The
  canonical-graph skip in the projector's `canonical_builder.go` removes plain
  `Variable` nodes from the graph projection but leaves ALL `Variable` rows —
  plain and semantic — in `content_entities`. Routing `variable` through the
  graph-first path (`graphFirstContentBackedEntityTypes`) is unsafe: a FEW
  semantic `Variable` graph nodes still exist (module attributes, TSX/Elixir
  component-type-assertion variables), and any non-empty graph result there
  short-circuits the content fallback in
  `queryGraphFirstContentByLanguageWithSemanticFilter`, silently omitting the
  plain variables the query is meant to surface. `entity_type=module_attribute`
  keeps the graph-first route since it legitimately wants the semantic graph
  node first.
- Repository-language inventory reads use the Postgres content index through
  `CountRepositoriesByLanguage`, `ReadRepositoriesByLanguage`, and
  `RepositoryLanguageInventory`. The by-language route uses one statement for
  the aggregate and bounded page when `limit > 0`; `limit=0` remains a
  count-only read without per-repository coverage fan-out.
- Repository source browsing reads optional `ListRepositoryRefs` support from
  the content store. `/branches` prefers those source-backed refs, while
  `/tree` and `/content` reject a selected ref that is unknown, unavailable, or
  not equal to the indexed commit instead of falling back silently.
- `queryContentStoreCoverage` uses `ContentReader.RepositoryCoverage` as the
  bounded count source when content rows are available. The graph count in
  `queryRepositoryGraphCoverageStats` is a no-content fallback, so
  `graph_gap_count` and `content_gap_count` stay zero when graph parity was not
  checked.
- `WriteSuccess` branches on `AcceptsEnvelope(r)` at `querycontract/http.go:38`;
  callers that do not send `Accept: application/eshu.envelope+json` receive the
  legacy payload shape. MCP tool dispatch relies on the envelope format; do not
  break this negotiation logic.
- Repository story and stats handlers use `WriteSuccess` so MCP can preserve
  `ResponseEnvelope` truth metadata while plain HTTP clients keep the legacy
  JSON body.
- Component extension inventory and diagnostics read only the configured local
  component registry on explicit API/MCP request.
- The OpenAPI spec is assembled from string fragments in Go source, not from
  runtime reflection. When a handler changes its request or response shape,
  update the matching `openapi/paths/<family>/` fragment in the same PR.

## Related docs

- [read-models.md](read-models.md) for route-specific read-model bounds,
  evidence, and investigation-route contracts.
- [dead-code-reachability.md](dead-code-reachability.md) for dead-code language roots, exactness blockers, and candidate paging rules.
- retired evidence record and dashboard correctness evidence (#5244)
  for issue-specific no-regression, performance, and observability proof.
- `docs/public/reference/http-api.md` for the public HTTP and envelope contract.
- `docs/public/reference/dead-code-reachability-spec.md` for the dead-code
  language maturity contract.
