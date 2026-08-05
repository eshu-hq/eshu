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

- **container_image_identity** (`go/internal/reducer/container_image_identity_writer.go:117-162`):
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

1. **ci_cd_run_correlation → PROJECT its exact workflow-image slice; FEED the
   remaining build signal**: exact decisions whose correlation kind is
   `workflow_image` project `BUILT_FROM` under the dedicated
   `reducer/ci-cd-run-correlation/workflow-image` evidence source (#5830).
   Artifact-only and non-exact decisions do not project. The broader
   build-provenance signal still reaches the graph through the
   container_image_identity lane:
   `addCICDArtifactImageReference` folds CI-run digest evidence into
   `BuildProvenanceRepositoryIDs`, and #5457's writer projects the resulting
   `BUILT_FROM` edge under `evidence_source=reducer/container-image-identity`.
   Relationship identity is `(start, end, type, scope_id, evidence_source)` on
   the #5827 writer and the NornicDB #290 source pin. Same-pair assertions from
   independent scopes or writers therefore coexist, and a scoped retract
   removes only its owner's assertion. Correlation truth (run identity, outcome
   tier, environment) stays Postgres-only and disclosed.

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
| ci_cd_run_correlation workflow images | PROJECT (exact, canonical container image only); remaining read-model stays Postgres-only (disclosed) | `reducer/ci-cd-run-correlation/workflow-image` | `BUILT_FROM` | #5428 (initially rescinded), #5827, #5830 |
| ci.job / ci.pipeline_definition / ci.warning | DISCLOSURE (registry comments) | N/A | N/A | #5428 |
| container_image_identity | PROJECT (exact_digest, BuildProvenanceRepositoryIDs non-empty) | `reducer/container-image-identity` | `BUILT_FROM` | #5457, gate narrowed by #5796, identity isolated by #5827 |
| container_image_identity base images | PROJECT (exact_digest on BOTH endpoints, single distinct base per repository) | `reducer/container-image-base-image` | `DERIVED_FROM` (ContainerImage → ContainerImage) | #5460 |
| container_image_identity workload/service ids | POSTGRES-ONLY (policy v1) | N/A | N/A | — |
| package ownership correlation | PROJECT (exact/derived, non-empty source ids) | `reducer/package-ownership` | `PUBLISHES` (Repository → Package/PackageVersion) | #5457 |
| package publication correlation | PROJECT (exact/derived, non-empty source ids) | `reducer/package-publication` | `PUBLISHES` (Repository → Package/PackageVersion) | #5457 |
| package consumption correlation | POSTGRES-ONLY (disclosed) | N/A | N/A | — |

### Base-image lineage tiering (#5460)

`DERIVED_FROM` carries `ContainerImage` on BOTH endpoints, and the canonical
writer matches a `ContainerImage` by digest. Decision #4's exact-only rule
therefore binds twice: an edge is projected only when the child AND the base
each resolve to `exact_digest`. This is not merely policy conformance — a
tag-only base has no `ContainerImage {digest}` node to point at, and the
question the edge exists to answer ("does my image inherit CVE-X from its
base?") is unanswerable unless the base resolves to the specific digest whose
vulnerabilities are known.

Two further rules are specific to this domain:

- **Runtime lineage is the FINAL Dockerfile stage only.** An intermediate
  builder stage does not ship its base OS into the runtime image; only the
  artifacts an explicit `COPY --from` names cross the stage boundary.
  Projecting a builder stage's base would assert CVE inheritance the image does
  not carry.
- **Attribution is conservative.** Dockerfile evidence names a repository's
  base but never says which of that repository's built images came from which
  Dockerfile. A repository resolving to more than one distinct base is
  ambiguous and projects NO edge, rather than an all-pairs fan-out that would
  fabricate lineage in a monorepo building several images from several
  Dockerfiles. A fabricated inheritance claim is a worse failure than a missing
  one.

Edges carry `attribution_basis` (today always `repository_single_base`) so a
later CI/SLSA per-image link can be admitted on the same edge type as a
strictly more precise basis, with no edge-type, schema, or query change.

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
| get_service_story | **none** — `evidence_boundaries` is omitted from the response | Both candidate domains are fully served: ci_cd_run_correlation via the top-level `ci_cd_evidence` field, and container_image_identity via `code_to_runtime_trace`'s `image_package` segment (`service_story_trace_path.go:94-121`, backed by `supply_chain_evidence`, `service_story_supply_chain.go:314-347`). `evidence_graph` alone still omits ci_cd/supply-chain GRAPH edges — `buildServiceEvidenceGraph` (`service_story_evidence_graph.go`) builds nodes/edges from the workload context, not from a BUILT_FROM graph read, so container_image_identity's BUILT_FROM edges (projected since #5457) are not wired into it — but that is a narrower sub-surface gap, not a whole-tool boundary, so it is not disclosed as one |
| get_workload_story | ci_cd_run_correlation, package_correlation_consumption | container_image_identity's BUILT_FROM projection (#5457) closed its gap; ci_cd_run_correlation stays Postgres-only, and package_correlation narrowed to its consumption slice (ownership/publication now project PUBLISHES) |
| get_repo_story | **none** — `evidence_boundaries` is omitted from the response | container_image_identity, package_correlation_ownership, and package_correlation_publication all now project canonical graph edges (BUILT_FROM, PUBLISHES), closing every prior boundary for this surface |
| trace_deployment_chain | ci_cd_run_correlation, container_image_identity | image_ref→digest identity + CI-run-produced-image hop invisible |

These disclosures are additive only — they add `evidence_boundaries` without
renaming or removing any existing fields. Entries are deterministic (stable
sort by domain) for golden assertions. The OpenAPI schema keeps
`evidence_boundaries` declared as an optional property on all four routes even
where, as with get_service_story today, no instance currently has a non-empty
boundary set — the schema documents what the route CAN return, not a
per-request guarantee.
