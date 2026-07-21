# Graph-Projection Policy for Postgres-Only Reducer Domains

Status: proposed decision for #5472.
Parent epic: #5470.
Related design issues: #5428 (ci/cd), #5450 (cloud), #5457 (artifact).

## Problem

Several reducer domains produce correlation evidence that lives in Postgres but
never reaches the graph. Story surfaces that read the graph for structure
(service_story, workload_story, repo_story, deployment_chain) omit whole
classes of truth because those edges are absent from the graph schema.

Domain-by-domain file:line evidence:

- **ci_cd_run_correlation** (`go/internal/reducer/ci_cd_run_correlation.go:309-319`):
  exact outcomes name `canonical_target: "container_image"` and carry
  `SourceLayerKinds` plus an `ImageRef`, but nothing materializes a graph
  edge. The `ContainerImage`/`OciImageManifest` node exists in the graph; the
  CI-run-produced-an-image relationship does not.

- **container_image_identity** (`go/internal/reducer/container_image_identity_writer.go:117-150`):
  canonical-only writer of image identity facts. `source_repository_ids`,
  `workload_ids`, and `service_ids` are stored but never projected as graph
  edges. The image node stands alone with no link to the repository that built
  it.

- **package correlations** (ownership, consumption, publication): Postgres-only
  facts in `go/internal/storage/postgres/facts_active_package_ownership.go` and
  `facts_active_package_consumption.go`. The graph has `Repository`, `Package`,
  and `PackageVersion` nodes but no `PUBLISHES` or `OWNS` edge writer.

- **RUNS_IMAGE**: the graph-only edge `(WorkloadInstance)-[:RUNS_IMAGE]->(OciImageManifest)`
  ties an instance to a digest, but the Postgres `container_image_identity` fact
  carries that same digest with zero joining code — the identity chain from
  repository to runtime image is broken at the graph layer.

The silent-omit surfaces identified in the terrain:

| Story surface | Omitted link | File:line |
| --- | --- | --- |
| get_service_story evidence_graph | ci_cd/supply-chain links in Postgres | `service_story_seam.go:83-90`, `service_story_supply_chain.go:322-347` |
| get_workload_story | ci_cd/image/package chain absent | `entity_workload_handlers.go:60-103` |
| get_repo_story | publication/ownership/image links | `repository_story.go` |
| trace_deployment_chain | image_ref→digest identity + which-CI-run-produced-image hop | `impact_trace_deployment.go`, `impact_trace_deployment_resources.go` |

## Decision

The spine domains get a graph-projection policy in this order:

1. **ci_cd_run_correlation → PROJECT**: exact outcomes with non-empty
   `ImageRef` and a matching `container_image_identity` row get a bounded
   graph write as `(ContainerImage|OciImageManifest)-[:BUILT_FROM]->(Repository)`
   with `evidence_source=reducer/ci-cd-run-correlation`. Implementer #5428.

   `ci.job`, `ci.pipeline_definition`, and `ci.warning` kinds: registry
   disclosure comments only (no silent dead weight) — these have no reducer
   decode call today and no exact-outcome path to project.

2. **container_image_identity → PROJECT**: exact_digest outcomes with non-empty
   `source_repository_ids` get `BUILT_FROM` (same edge type,
   `evidence_source=reducer/container-image-identity`).
   `workload_ids`/`service_ids` stay Postgres-only in policy v1 (no graph
   workload join). Implementer #5457.

3. **package correlations → PROJECT**: ownership and publication (exact/derived
   with non-empty source ids) as `(:Repository)-[:PUBLISHES]->(:Package|:PackageVersion)`.
   Consumption correlation STAYS Postgres-only — it overlaps the existing
   `DECLARES_DEPENDENCY`/`DEPENDS_ON` graph lanes — so the boundary is
   DISCLOSED instead. Implementer #5457.

4. **All non-exact outcomes** (derived/ambiguous/unresolved/stale/rejected)
   stay provenance-only Postgres everywhere. Exact-only promotion is the rule,
   mirroring `kubernetes_correlation_edge_rows.go`'s exact-only extraction.

### Per-domain disposition

| Domain | Decision | Evidence source | Edge type | Implementer |
| --- | --- | --- | --- | --- |
| ci_cd_run_correlation | PROJECT (exact only) | `reducer/ci-cd-run-correlation` | `BUILT_FROM` (ContainerImage/OciImageManifest → Repository) | #5428 |
| ci.job / ci.pipeline_definition / ci.warning | DISCLOSURE (registry comments) | N/A | N/A | #5428 |
| container_image_identity | PROJECT (exact_digest, source_repository_ids non-empty) | `reducer/container-image-identity` | `BUILT_FROM` (same edge, distinct source) | #5457 |
| container_image_identity workload/service ids | POSTGRES-ONLY (policy v1) | N/A | N/A | — |
| package ownership correlation | PROJECT (exact/derived, non-empty source ids) | `reducer/package-ownership` | `PUBLISHES` (Repository → Package/PackageVersion) | #5457 |
| package publication correlation | PROJECT (exact/derived, non-empty source ids) | `reducer/package-publication` | `PUBLISHES` (Repository → Package/PackageVersion) | #5457 |
| package consumption correlation | POSTGRES-ONLY (disclosed) | N/A | N/A | — |

### Retraction path

Edges are stamped with `scope_id`, `generation_id`, and `evidence_source`.
Retraction uses retract-first per generation with a `scope + evidence_source`
predicate, sequential autocommit dispatch (NornicDB DELETE-under-transaction
bug — cite `kubernetes_correlation_edge_writer.go:250-267`). New entries go
in `retractable_edge_types.go` + `specs/replay-depth-requirements.v1.yaml`
plus a replay delta scenario.

### Cost budget

- **Exact-outcome-only promotion**: caps write volume to exact matches only.
- **DefaultBatchSize 500**: standard writer batch size.
- **Two-MATCH-MERGE**: `MATCH (a), MATCH (b) MERGE (a)-[:BUILT_FROM]->(b)` —
  missing endpoints are a no-op (never fabricate nodes).
- **Materialized/skipped tallies**: observable via reducer completion logs.
- **B-9 handler budget gate**: entries in `testdata/benchmarks/reducer-handler-budgets.txt`.
- **Each implementer PR** must show a measured write-volume perf table.

### Truth contracts

Each PROJECT domain gets its own additive `truth.Contract` as a sibling
materialization domain, mirroring the split between `kubernetes_correlation`
and `kubernetes_correlation_materialization`. The three existing domain
contracts (`ci_cd_run_correlation`, `container_image_identity`, `package_*`)
are unchanged — the graph-projection contracts are new and additive.

### Disclosure rule

Any graph-sourced story section that omits a Postgres-only chain link MUST
carry a boundary disclosure naming the domain and its read surface.
Disclosures are STATIC boundary declarations — per-request Postgres presence
probes are forbidden without a measured budget.

The disclosure vocabulary is `PostgresOnlyBoundary`:
```json
{
  "domain": "container_image_identity",
  "read_surface": "get_workload_story",
  "reason": "postgres_only_read_model"
}
```

A domain is disclosed for a read surface only when it is genuinely absent from
that surface's ENTIRE response — top-level fields and nested structures alike.
A domain already served by a sibling field (for example get_service_story's
top-level `ci_cd_evidence`, or `code_to_runtime_trace`'s `image_package`
segment, which embeds `container_image_identity` evidence read back from
`supply_chain_evidence`) is never disclosed as a boundary for that surface —
there is no omission to disclose. get_service_story's boundary set is
currently empty for exactly this reason: see the surface mappings below.

## Consequences

This policy codifies which domains project and which stay Postgres-only. It
gates all three implementer PRs through the same edge-type contract, retraction
discipline, cost budget, and disclosure rule.

The cost is that three domains must implement bounded graph writes with
retraction and telemetry. The benefit is that story surfaces can surface the
BUILT_FROM, PUBLISHES, and RUNS_IMAGE chains without ad-hoc fixups, and the
retraction/replay discipline prevents stale edges from accumulating.

The disclosure rule adds a lightweight, optional `evidence_boundaries` field
to four story surfaces' OpenAPI schemas (implemented in this PR); the field is
populated only when a surface has a genuine, currently-undisclosed boundary,
and omitted entirely otherwise (get_service_story: see below). The
implementer PRs then remove those boundaries as domains project — a boundary
that vanishes from the code is documented in the PR as "domain X no longer
postgres-only."

The three registries-only disclosure comments for `ci.job`,
`ci.pipeline_definition`, and `ci.warning` are pure documentation text in
`specs/fact-kind-registry.v1.yaml`, same class as #5475. They carry no
runtime cost and no graph schema change.

## Disclosure surface mappings

| Story tool | Boundary domains | Reason |
| --- | --- | --- |
| get_service_story | **none** — `evidence_boundaries` is omitted from the response | Both candidate domains are fully served: ci_cd_run_correlation via the top-level `ci_cd_evidence` field, and container_image_identity via `code_to_runtime_trace`'s `image_package` segment (`service_story_trace_path.go:94-121`, backed by `supply_chain_evidence`, `service_story_supply_chain.go:314-347`). `evidence_graph` alone omits ci_cd/supply-chain GRAPH edges (no BUILT_FROM edge is projected yet), but that is a narrower sub-surface gap, not a whole-tool boundary, so it is not disclosed as one |
| get_workload_story | ci_cd_run_correlation, container_image_identity, package_correlation | entire ci_cd/image/package chain absent from workload graph read |
| get_repo_story | container_image_identity, package_correlation_ownership, package_correlation_publication | no publication/ownership/image links in graph repo story |
| trace_deployment_chain | ci_cd_run_correlation, container_image_identity | image_ref→digest identity + CI-run-produced-image hop invisible |

These disclosures are additive only — they add `evidence_boundaries` without
renaming or removing any existing fields. Entries are deterministic (stable
sort by domain) for golden assertions. The OpenAPI schema keeps
`evidence_boundaries` declared as an optional property on all four routes even
where, as with get_service_story today, no instance currently has a non-empty
boundary set — the schema documents what the route CAN return, not a
per-request guarantee.
